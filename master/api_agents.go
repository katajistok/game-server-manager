package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func handleAgents(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listAgents(hub, db, w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func listAgents(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, name, status, last_seen_at, host_info, created_at
		FROM agents
		WHERE status != 'pending'
		ORDER BY name
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query agents")
		return
	}
	defer rows.Close()

	type AgentRow struct {
		ID         int             `json:"id"`
		Name       string          `json:"name"`
		Status     string          `json:"status"`
		LastSeenAt *time.Time      `json:"last_seen_at"`
		HostInfo   json.RawMessage `json:"host_info"`
		CreatedAt  time.Time       `json:"created_at"`
		Connected  bool            `json:"connected"`
	}

	agents := []AgentRow{}
	for rows.Next() {
		var a AgentRow
		var hostInfo []byte
		err := rows.Scan(&a.ID, &a.Name, &a.Status, &a.LastSeenAt, &hostInfo, &a.CreatedAt)
		if err != nil {
			continue
		}
		a.HostInfo = hostInfo
		hub.mu.RLock()
		_, a.Connected = hub.Agents[a.Name]
		hub.mu.RUnlock()
		agents = append(agents, a)
	}

	writeJSON(w, http.StatusOK, agents)
}

func handlePendingAgents(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`
			SELECT id, name, status, host_info, last_seen_at
			FROM agents WHERE status = 'pending' ORDER BY last_seen_at DESC
		`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		type PendingRow struct {
			ID         int             `json:"id"`
			Name       string          `json:"name"`
			Status     string          `json:"status"`
			HostInfo   json.RawMessage `json:"host_info"`
			LastSeenAt *time.Time      `json:"last_seen_at"`
			Connected  bool            `json:"connected"`
		}

		pending := []PendingRow{}
		for rows.Next() {
			var p PendingRow
			var hostInfo []byte
			if err := rows.Scan(&p.ID, &p.Name, &p.Status, &hostInfo, &p.LastSeenAt); err != nil {
				continue
			}
			p.HostInfo = hostInfo
			hub.mu.RLock()
			_, p.Connected = hub.PendingAgents[p.Name]
			hub.mu.RUnlock()
			pending = append(pending, p)
		}
		writeJSON(w, http.StatusOK, pending)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleAgentByID(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing agent id")
		return
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "approve" && r.Method == http.MethodPost:
		approveAgentHandler(hub, db, w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		deleteAgentHandler(db, w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func approveAgentHandler(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	var name string
	err := db.QueryRow(`SELECT name FROM agents WHERE id = $1`, id).Scan(&name)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	token, err := approveAgent(db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve agent")
		return
	}

	hub.ApprovePendingAgent(name, token, id, db)

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "approved",
		"token":  token,
		"note":   "Save this token in AGENT_TOKEN env var for future reconnects",
	})
}

func deleteAgentHandler(db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	_, err := db.Exec(`DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
