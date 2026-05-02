package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

type Hub struct {
	Agents        map[string]*AgentConnection
	UIClients     map[*websocket.Conn]bool
	PendingAgents map[string]*PendingAgent
	AgentMessages chan AgentMessage
	UIMessages    chan UIMessage
	RegisterAgent   chan *AgentConnection
	UnregisterAgent chan *AgentConnection
	RegisterUI   chan *websocket.Conn
	UnregisterUI chan *websocket.Conn

	// Latest metrics per agent — stored in memory, relayed to UI
	LatestMetrics map[string]*MetricsPayload
	MetricsHistory map[string][]MetricsPayload // last 30 data points per agent

	mu sync.RWMutex
}

type PendingAgent struct {
	Name string
	Conn *websocket.Conn
	Info HostInfo
}

type AgentConnection struct {
	AgentID   int
	AgentName string
	Conn      *websocket.Conn
	Send      chan []byte
	mu        sync.Mutex
}

type AgentMessage struct {
	Agent   *AgentConnection
	Message []byte
}

type UIMessage struct {
	Conn    *websocket.Conn
	Message []byte
}

func NewHub() *Hub {
	return &Hub{
		Agents:          make(map[string]*AgentConnection),
		UIClients:       make(map[*websocket.Conn]bool),
		PendingAgents:   make(map[string]*PendingAgent),
		AgentMessages:   make(chan AgentMessage, 256),
		UIMessages:      make(chan UIMessage, 256),
		RegisterAgent:   make(chan *AgentConnection),
		UnregisterAgent: make(chan *AgentConnection),
		RegisterUI:      make(chan *websocket.Conn),
		UnregisterUI:    make(chan *websocket.Conn),
		LatestMetrics:   make(map[string]*MetricsPayload),
		MetricsHistory:  make(map[string][]MetricsPayload),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case agent := <-h.RegisterAgent:
			h.mu.Lock()
			h.Agents[agent.AgentName] = agent
			h.mu.Unlock()
			log.Printf("[hub] Agent registered: %s", agent.AgentName)

		case agent := <-h.UnregisterAgent:
			h.mu.Lock()
			if _, ok := h.Agents[agent.AgentName]; ok {
				delete(h.Agents, agent.AgentName)
				close(agent.Send)
				log.Printf("[hub] Agent unregistered: %s", agent.AgentName)
			}
			h.mu.Unlock()

		case conn := <-h.RegisterUI:
			h.mu.Lock()
			h.UIClients[conn] = true
			h.mu.Unlock()
			log.Printf("[hub] UI client connected")

		case conn := <-h.UnregisterUI:
			h.mu.Lock()
			if _, ok := h.UIClients[conn]; ok {
				delete(h.UIClients, conn)
				conn.Close()
				log.Printf("[hub] UI client disconnected")
			}
			h.mu.Unlock()

		case msg := <-h.AgentMessages:
			h.mu.RLock()
			for conn := range h.UIClients {
				err := conn.WriteMessage(websocket.TextMessage, msg.Message)
				if err != nil {
					log.Printf("[hub] Error writing to UI client: %v", err)
				}
			}
			h.mu.RUnlock()

		case msg := <-h.UIMessages:
			h.mu.RLock()
			for _, agent := range h.Agents {
				agent.Send <- msg.Message
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) StoreMetrics(agentName string, m MetricsPayload) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.LatestMetrics[agentName] = &m

	// Keep last 30 data points
	history := h.MetricsHistory[agentName]
	history = append(history, m)
	if len(history) > 30 {
		history = history[len(history)-30:]
	}
	h.MetricsHistory[agentName] = history
}

func (h *Hub) GetMetricsHistory(agentName string) []MetricsPayload {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.MetricsHistory[agentName]
}

func (h *Hub) BroadcastToUI(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.UIClients {
		conn.WriteMessage(websocket.TextMessage, message)
	}
}

func (h *Hub) SendToAgent(agentName string, message []byte) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	agent, ok := h.Agents[agentName]
	if !ok {
		return false
	}
	agent.Send <- message
	return true
}

func (h *Hub) AddPending(p *PendingAgent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.PendingAgents[p.Name] = p
	log.Printf("[hub] Pending agent added: %s", p.Name)
}

func (h *Hub) RemovePending(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.PendingAgents, name)
}

func (h *Hub) ApprovePendingAgent(name string, token string, agentID int, db *sql.DB) bool {
	h.mu.Lock()
	pending, ok := h.PendingAgents[name]
	if !ok {
		h.mu.Unlock()
		return false
	}
	delete(h.PendingAgents, name)
	h.mu.Unlock()

	msg, _ := json.Marshal(Envelope{
		Type:    MsgApproved,
		Payload: ApprovedPayload{Token: token},
	})
	pending.Conn.WriteMessage(websocket.TextMessage, msg)

	agent := &AgentConnection{
		AgentID:   agentID,
		AgentName: name,
		Conn:      pending.Conn,
		Send:      make(chan []byte, 256),
	}

	updateAgentStatus(db, name, "online", pending.Info)
	h.RegisterAgent <- agent
	go agentWriter(agent, h, db)
	go agentReader(agent, h, db)

	return true
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	hub := NewHub()
	go hub.Run()

	db, err := initDB()
	if err != nil {
		log.Fatalf("[main] Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Printf("[main] Database connected")

	mux := http.NewServeMux()

	mux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) {
		handleAgentWS(hub, db, w, r)
	})
	mux.HandleFunc("/ws/ui", func(w http.ResponseWriter, r *http.Request) {
		handleUIWS(hub, w, r)
	})

	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/agents/pending", func(w http.ResponseWriter, r *http.Request) {
		handlePendingAgents(hub, db, w, r)
	})
	mux.HandleFunc("/api/agents/", func(w http.ResponseWriter, r *http.Request) {
		handleAgentByID(hub, db, w, r)
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		handleAgents(hub, db, w, r)
	})
	mux.HandleFunc("/api/game-definitions", func(w http.ResponseWriter, r *http.Request) {
		handleGameDefinitions(db, w, r)
	})
	mux.HandleFunc("/api/servers/", func(w http.ResponseWriter, r *http.Request) {
		handleServerByID(hub, db, w, r)
	})
	mux.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		handleServers(hub, db, w, r)
	})
	mux.HandleFunc("/api/metrics/", func(w http.ResponseWriter, r *http.Request) {
		handleMetrics(hub, w, r)
	})

	log.Printf("[main] Master server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("[main] Server failed: %v", err)
	}
}
