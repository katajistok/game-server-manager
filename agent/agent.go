package main

import (
	"encoding/json"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type Config struct {
	MasterURL string
	AgentName string
	Token     string
}

type Agent struct {
	cfg     Config
	conn    *websocket.Conn
	send    chan []byte
	stopped bool
	mu      sync.Mutex
	docker  *DockerManager
}

func NewAgent(cfg Config) *Agent {
	return &Agent{
		cfg:    cfg,
		send:   make(chan []byte, 256),
		docker: NewDockerManager(),
	}
}

func (a *Agent) Stop() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
}

func (a *Agent) Connect() error {
	log.Printf("[agent] Dialing %s", a.cfg.MasterURL)
	conn, _, err := websocket.DefaultDialer.Dial(a.cfg.MasterURL, nil)
	if err != nil {
		return err
	}
	log.Printf("[agent] Dial successful")

	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()

	log.Printf("[agent] Connected to master")

	log.Printf("[agent] Sending register...")
	if err := a.register(); err != nil {
		conn.Close()
		return err
	}
	log.Printf("[agent] Register sent successfully")

	go a.writer()
	go a.heartbeat()
	go func() {
		log.Printf("[metrics] Goroutine started")
		time.Sleep(2 * time.Second)
		log.Printf("[metrics] Starting metrics loop")
		a.metricsLoop()
	}()
	log.Printf("[agent] All goroutines started, entering reader...")
	a.reader()
	log.Printf("[agent] Reader exited")
	return nil
}

func (a *Agent) register() error {
	payload := RegisterPayload{
		AgentName: a.cfg.AgentName,
		Token:     a.cfg.Token,
		HostInfo:  getHostInfo(),
	}
	msg, err := json.Marshal(Envelope{
		Type:    MsgRegister,
		Payload: payload,
	})
	if err != nil {
		return err
	}
	return a.conn.WriteMessage(websocket.TextMessage, msg)
}

func (a *Agent) reader() {
	defer func() {
		log.Printf("[agent] Reader cleaning up")
		a.conn.Close()
	}()

	a.conn.SetReadLimit(512 * 1024)
	a.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	a.conn.SetPongHandler(func(string) error {
		a.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})

	for {
		_, message, err := a.conn.ReadMessage()
		if err != nil {
			log.Printf("[agent] Read error: %v", err)
			return
		}
		var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("[agent] Invalid message: %v", err)
			continue
		}
		go a.handleMessage(env)
	}
}

func (a *Agent) writer() {
	log.Printf("[agent] Writer running")
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		a.conn.Close()
	}()

	for {
		select {
		case message, ok := <-a.send:
			a.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				a.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := a.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[agent] Write error: %v", err)
				return
			}
		case <-ticker.C:
			a.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := a.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (a *Agent) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		a.mu.Lock()
		stopped := a.stopped
		a.mu.Unlock()
		if stopped {
			return
		}
		a.sendMessage(MsgHeartbeat, HeartbeatPayload{AgentName: a.cfg.AgentName})
	}
}

func (a *Agent) metricsLoop() {
	log.Printf("[metrics] Loop running")
	a.collectAndSendMetrics()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		a.mu.Lock()
		stopped := a.stopped
		a.mu.Unlock()
		if stopped {
			return
		}
		a.collectAndSendMetrics()
	}
}

func (a *Agent) sendMessage(msgType MessageType, payload interface{}) {
	a.mu.Lock()
	stopped := a.stopped
	a.mu.Unlock()
	if stopped {
		return
	}
	msg, err := json.Marshal(Envelope{
		Type:    msgType,
		Payload: payload,
	})
	if err != nil {
		log.Printf("[agent] Failed to marshal message: %v", err)
		return
	}
	select {
	case a.send <- msg:
	default:
		log.Printf("[agent] sendMessage dropped (channel full): %s", msgType)
	}
}

func (a *Agent) handleMessage(env Envelope) {
	payloadBytes, err := json.Marshal(env.Payload)
	if err != nil {
		log.Printf("[agent] Failed to re-marshal payload: %v", err)
		return
	}

	switch env.Type {
	case MsgStartServer:
		var p StartServerPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			log.Printf("[agent] Invalid start_server payload: %v", err)
			return
		}
		a.handleStartServer(p)

	case MsgStopServer:
		var p StopServerPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			log.Printf("[agent] Invalid stop_server payload: %v", err)
			return
		}
		a.handleStopServer(p)

	case MsgDeleteServer:
		var p DeleteServerPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			log.Printf("[agent] Invalid delete_server payload: %v", err)
			return
		}
		a.handleDeleteServer(p)

	case MsgRconCommand:
		var p RconCommandPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			log.Printf("[agent] Invalid rcon_command payload: %v", err)
			return
		}
		a.handleRcon(p)

	case MsgStreamLogsStart:
		var p StreamLogsPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			log.Printf("[agent] Invalid stream_logs_start payload: %v", err)
			return
		}
		go a.handleStreamLogs(p)

	case MsgStreamLogsStop:
		var p StreamLogsPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			return
		}
		a.docker.StopLogStream(p.ServerID)

	case MsgApproved:
		var p ApprovedPayload
		if err := json.Unmarshal(payloadBytes, &p); err != nil {
			log.Printf("[agent] Invalid approved payload: %v", err)
			return
		}
		log.Printf("[agent] Approved by master! Token: %s", p.Token)
		os.WriteFile("/app/agent.token", []byte(p.Token), 0600)
		a.cfg.Token = p.Token

	case MsgRequestMetrics:
		go a.collectAndSendMetrics()

	default:
		log.Printf("[agent] Unknown message type: %s", env.Type)
	}
}

func (a *Agent) handleStartServer(p StartServerPayload) {
	log.Printf("[agent] Starting server %d (%s)", p.ServerID, p.ContainerName)
	containerID, err := a.docker.StartContainer(p)
	if err != nil {
		log.Printf("[agent] Failed to start container: %v", err)
		a.sendMessage(MsgServerStatus, ServerStatusPayload{
			ServerID: p.ServerID,
			Status:   "error",
			Error:    err.Error(),
		})
		return
	}
	log.Printf("[agent] Container started: %s", containerID)
	a.sendMessage(MsgServerStatus, ServerStatusPayload{
		ServerID:    p.ServerID,
		ContainerID: containerID,
		Status:      "running",
	})
}

func (a *Agent) handleStopServer(p StopServerPayload) {
	log.Printf("[agent] Stopping server %d (%s)", p.ServerID, p.ContainerName)
	if err := a.docker.StopContainer(p.ContainerName); err != nil {
		log.Printf("[agent] Failed to stop container: %v", err)
		a.sendMessage(MsgServerStatus, ServerStatusPayload{
			ServerID: p.ServerID,
			Status:   "error",
			Error:    err.Error(),
		})
		return
	}
	a.sendMessage(MsgServerStatus, ServerStatusPayload{
		ServerID: p.ServerID,
		Status:   "stopped",
	})
}

func (a *Agent) handleDeleteServer(p DeleteServerPayload) {
	log.Printf("[agent] Deleting server %d (%s)", p.ServerID, p.ContainerName)
	if err := a.docker.DeleteContainer(p.ContainerName, p.VolumeName); err != nil {
		log.Printf("[agent] Failed to delete server: %v", err)
		a.sendMessage(MsgServerStatus, ServerStatusPayload{
			ServerID: p.ServerID,
			Status:   "error",
			Error:    err.Error(),
		})
		return
	}
	a.sendMessage(MsgServerStatus, ServerStatusPayload{
		ServerID: p.ServerID,
		Status:   "deleted",
	})
}

func (a *Agent) handleRcon(p RconCommandPayload) {
	log.Printf("[agent] RCON server %d: %s", p.ServerID, p.Command)
	response, err := sendRconCommand(p.RconHost, p.RconPort, p.RconPassword, p.Command)
	if err != nil {
		a.sendMessage(MsgRconResponse, RconResponsePayload{
			ServerID:  p.ServerID,
			MessageID: p.MessageID,
			Error:     err.Error(),
		})
		return
	}
	a.sendMessage(MsgRconResponse, RconResponsePayload{
		ServerID:  p.ServerID,
		MessageID: p.MessageID,
		Response:  response,
	})
}

func (a *Agent) handleStreamLogs(p StreamLogsPayload) {
	log.Printf("[agent] Starting log stream for server %d", p.ServerID)
	err := a.docker.StreamLogs(p.ServerID, p.ContainerName, func(line string) {
		a.sendMessage(MsgLogLine, LogLinePayload{
			ServerID: p.ServerID,
			Line:     line,
			Time:     time.Now().Format(time.RFC3339),
		})
	})
	if err != nil {
		log.Printf("[agent] Log stream error for server %d: %v", p.ServerID, err)
	}
}

func getHostInfo() HostInfo {
	info := HostInfo{
		OS:       runtime.GOOS,
		CPUCores: runtime.NumCPU(),
	}
	if m, err := mem.VirtualMemory(); err == nil {
		info.MemoryGB = int(m.Total / 1024 / 1024 / 1024)
	}
	path := "/"
	if p := os.Getenv("HOST_PROC"); p != "" {
		path = "/host"
	}
	if d, err := disk.Usage(path); err == nil {
		info.DiskGB = int(d.Total / 1024 / 1024 / 1024)
	}
	return info
}
