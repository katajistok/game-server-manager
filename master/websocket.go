package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 120 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 512 * 1024
)

func handleAgentWS(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws/agent] Upgrade error: %v", err)
		return
	}

	log.Printf("[ws/agent] New connection from %s", r.RemoteAddr)

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, rawMsg, err := conn.ReadMessage()
	if err != nil {
		log.Printf("[ws/agent] Failed to read register message: %v", err)
		conn.Close()
		return
	}

	var env Envelope
	if err := json.Unmarshal(rawMsg, &env); err != nil {
		log.Printf("[ws/agent] Invalid register message: %v", err)
		conn.Close()
		return
	}

	if env.Type != MsgRegister {
		log.Printf("[ws/agent] Expected register, got: %s", env.Type)
		conn.Close()
		return
	}

	payloadBytes, _ := json.Marshal(env.Payload)
	var reg RegisterPayload
	if err := json.Unmarshal(payloadBytes, &reg); err != nil {
		log.Printf("[ws/agent] Invalid register payload: %v", err)
		conn.Close()
		return
	}

	var storedToken, status string
	var agentID int
	err = db.QueryRow(`
		SELECT id, token, status FROM agents WHERE name = $1
	`, reg.AgentName).Scan(&agentID, &storedToken, &status)

	if err == sql.ErrNoRows || status == "pending" {
		if err == sql.ErrNoRows {
			if err := upsertPendingAgent(db, reg.AgentName, reg.HostInfo); err != nil {
				log.Printf("[ws/agent] Failed to upsert pending agent: %v", err)
				conn.Close()
				return
			}
		}
		log.Printf("[ws/agent] Agent pending approval: %s", reg.AgentName)
		pendingAgent := &PendingAgent{
			Name: reg.AgentName,
			Conn: conn,
			Info: reg.HostInfo,
		}
		hub.AddPending(pendingAgent)
		waitForApproval(hub, db, pendingAgent)
		return
	}

	if err != nil {
		log.Printf("[ws/agent] DB error: %v", err)
		conn.Close()
		return
	}

	if status == "approved" && storedToken != reg.Token {
		log.Printf("[ws/agent] Invalid token for agent: %s", reg.AgentName)
		conn.Close()
		return
	}

	updateAgentStatus(db, reg.AgentName, "online", reg.HostInfo)

	agent := &AgentConnection{
		AgentID:   agentID,
		AgentName: reg.AgentName,
		Conn:      conn,
		Send:      make(chan []byte, 256),
	}

	hub.RegisterAgent <- agent
	go agentWriter(agent, hub, db)
	agentReader(agent, hub, db)
}

func waitForApproval(hub *Hub, db *sql.DB, pending *PendingAgent) {
	defer func() {
		hub.RemovePending(pending.Name)
		pending.Conn.Close()
	}()

	pending.Conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
	for {
		_, _, err := pending.Conn.ReadMessage()
		if err != nil {
			log.Printf("[ws/agent] Pending agent %s disconnected", pending.Name)
			return
		}
	}
}

func agentReader(agent *AgentConnection, hub *Hub, db *sql.DB) {
	defer func() {
		hub.UnregisterAgent <- agent
		updateAgentStatus(db, agent.AgentName, "offline", HostInfo{})
		agent.Conn.Close()
	}()

	agent.Conn.SetReadLimit(maxMessageSize)
	agent.Conn.SetReadDeadline(time.Now().Add(pongWait))
	agent.Conn.SetPongHandler(func(string) error {
		agent.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := agent.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				log.Printf("[ws/agent] Read error for %s: %v", agent.AgentName, err)
			}
			break
		}

		var env Envelope
		if err := json.Unmarshal(message, &env); err == nil {
			switch env.Type {

			case MsgHeartbeat:
				updateAgentLastSeen(db, agent.AgentName)
				continue

			case MsgServerStatus:
				payloadBytes, _ := json.Marshal(env.Payload)
				var status ServerStatusPayload
				if err := json.Unmarshal(payloadBytes, &status); err == nil {
					log.Printf("[ws/agent] Server %d status: %s", status.ServerID, status.Status)
					if status.ContainerID != "" {
						db.Exec(`
							UPDATE game_servers
							SET status = $1, container_id = $2, updated_at = NOW()
							WHERE id = $3
						`, status.Status, status.ContainerID, status.ServerID)
					} else {
						db.Exec(`
							UPDATE game_servers
							SET status = $1, updated_at = NOW()
							WHERE id = $2
						`, status.Status, status.ServerID)
					}
				}

			case MsgMetrics:
				payloadBytes, _ := json.Marshal(env.Payload)
				var metrics MetricsPayload
				if err := json.Unmarshal(payloadBytes, &metrics); err == nil {
					hub.StoreMetrics(agent.AgentName, metrics)
					// Relay to UI so it updates live
				}
			}
		}

		// Relay all messages to UI clients
		hub.AgentMessages <- AgentMessage{Agent: agent, Message: message}
	}
}

func agentWriter(agent *AgentConnection, hub *Hub, db *sql.DB) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		agent.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-agent.Send:
			agent.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				agent.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := agent.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[ws/agent] Write error for %s: %v", agent.AgentName, err)
				return
			}

		case <-ticker.C:
			agent.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := agent.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func handleUIWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws/ui] Upgrade error: %v", err)
		return
	}

	hub.RegisterUI <- conn
	log.Printf("[ws/ui] UI client connected from %s", r.RemoteAddr)

	defer func() {
		hub.UnregisterUI <- conn
	}()

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				log.Printf("[ws/ui] Read error: %v", err)
			}
			break
		}
		hub.UIMessages <- UIMessage{Conn: conn, Message: message}
	}
}
