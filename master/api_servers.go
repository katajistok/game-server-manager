package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func handleServers(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listServers(db, w, r)
	case http.MethodPost:
		createServer(db, w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleServerByID(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/servers/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing server id")
		return
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		getServer(db, w, r, id)
	case action == "" && r.Method == http.MethodPut:
		updateServer(db, w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		deleteServer(hub, db, w, r, id)
	case action == "start" && r.Method == http.MethodPost:
		startServer(hub, db, w, r, id)
	case action == "stop" && r.Method == http.MethodPost:
		stopServer(hub, db, w, r, id)
	case action == "rcon" && r.Method == http.MethodPost:
		sendRcon(hub, db, w, r, id)
	case action == "logs" && r.Method == http.MethodGet:
		getServerLogs(db, w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func listServers(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			gs.id, gs.name, gs.status, gs.port, gs.max_players,
			gs.password IS NOT NULL AND gs.password != '' as has_password,
			gs.container_id, gs.container_name,
			gs.created_at, gs.updated_at,
			gd.name as game_name, gd.slug as game_slug,
			gm.name as mode_name, gm.slug as mode_slug,
			a.name as agent_name
		FROM game_servers gs
		JOIN game_definitions gd ON gd.id = gs.game_definition_id
		LEFT JOIN game_modes gm ON gm.id = gs.game_mode_id
		JOIN agents a ON a.id = gs.agent_id
		ORDER BY gs.id
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query servers")
		return
	}
	defer rows.Close()

	type ServerRow struct {
		ID            int       `json:"id"`
		Name          string    `json:"name"`
		Status        string    `json:"status"`
		Port          int       `json:"port"`
		MaxPlayers    int       `json:"max_players"`
		HasPassword   bool      `json:"has_password"`
		ContainerID   *string   `json:"container_id"`
		ContainerName string    `json:"container_name"`
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
		GameName      string    `json:"game_name"`
		GameSlug      string    `json:"game_slug"`
		ModeName      *string   `json:"mode_name"`
		ModeSlug      *string   `json:"mode_slug"`
		AgentName     string    `json:"agent_name"`
	}

	servers := []ServerRow{}
	for rows.Next() {
		var s ServerRow
		err := rows.Scan(
			&s.ID, &s.Name, &s.Status, &s.Port, &s.MaxPlayers,
			&s.HasPassword, &s.ContainerID, &s.ContainerName,
			&s.CreatedAt, &s.UpdatedAt,
			&s.GameName, &s.GameSlug,
			&s.ModeName, &s.ModeSlug,
			&s.AgentName,
		)
		if err != nil {
			log.Printf("[api] scan error: %v", err)
			continue
		}
		servers = append(servers, s)
	}

	writeJSON(w, http.StatusOK, servers)
}

func getServer(db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	var s struct {
		ID            int                `json:"id"`
		Name          string             `json:"name"`
		Status        string             `json:"status"`
		Port          int                `json:"port"`
		MaxPlayers    int                `json:"max_players"`
		Password      *string            `json:"password,omitempty"`
		RconPassword  *string            `json:"rcon_password,omitempty"`
		ContainerID   *string            `json:"container_id"`
		ContainerName string             `json:"container_name"`
		GameName      string             `json:"game_name"`
		GameSlug      string             `json:"game_slug"`
		ModeName      *string            `json:"mode_name"`
		AgentName     string             `json:"agent_name"`
		CustomConfig  map[string]string  `json:"custom_config"`
	}

	err := db.QueryRow(`
		SELECT
			gs.id, gs.name, gs.status, gs.port, gs.max_players,
			gs.password, gs.rcon_password,
			gs.container_id, gs.container_name,
			gd.name, gd.slug, gm.name, a.name
		FROM game_servers gs
		JOIN game_definitions gd ON gd.id = gs.game_definition_id
		LEFT JOIN game_modes gm ON gm.id = gs.game_mode_id
		JOIN agents a ON a.id = gs.agent_id
		WHERE gs.id = $1
	`, id).Scan(
		&s.ID, &s.Name, &s.Status, &s.Port, &s.MaxPlayers,
		&s.Password, &s.RconPassword,
		&s.ContainerID, &s.ContainerName,
		&s.GameName, &s.GameSlug, &s.ModeName, &s.AgentName,
	)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	// Fetch custom config from server_configs
	s.CustomConfig = map[string]string{}
	cfgRows, err := db.Query(`SELECT key, value FROM server_configs WHERE game_server_id = $1`, id)
	if err == nil {
		defer cfgRows.Close()
		for cfgRows.Next() {
			var k, v string
			if cfgRows.Scan(&k, &v) == nil {
				s.CustomConfig[k] = v
			}
		}
	}

	writeJSON(w, http.StatusOK, s)
}

func createServer(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID          int               `json:"agent_id"`
		GameDefinitionID int               `json:"game_definition_id"`
		GameModeID       *int              `json:"game_mode_id"`
		Name             string            `json:"name"`
		Port             int               `json:"port"`
		MaxPlayers       int               `json:"max_players"`
		Password         string            `json:"password"`
		RconPassword     string            `json:"rcon_password"`
		CustomConfig     map[string]string `json:"custom_config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Port == 0 || req.AgentID == 0 || req.GameDefinitionID == 0 {
		writeError(w, http.StatusBadRequest, "name, port, agent_id and game_definition_id are required")
		return
	}

	containerName := "lanmaster-" + strings.ToLower(
		strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(req.Name),
	)

	var password, rconPassword interface{}
	if req.Password != "" {
		password = req.Password
	}
	if req.RconPassword != "" {
		rconPassword = req.RconPassword
	}

	var id int
	err := db.QueryRow(`
		INSERT INTO game_servers
			(agent_id, game_definition_id, game_mode_id, name, container_name,
			 port, max_players, password, rcon_password)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`,
		req.AgentID, req.GameDefinitionID, req.GameModeID,
		req.Name, containerName,
		req.Port, req.MaxPlayers,
		password, rconPassword,
	).Scan(&id)

	if err != nil {
		log.Printf("[api] create server error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create server")
		return
	}

	// Insert custom config entries
	for k, v := range req.CustomConfig {
		if v == "" {
			continue
		}
		_, err := db.Exec(`
			INSERT INTO server_configs (game_server_id, key, value)
			VALUES ($1, $2, $3)
			ON CONFLICT (game_server_id, key) DO NOTHING
		`, id, k, v)
		if err != nil {
			log.Printf("[api] insert server_config error for key %s: %v", k, err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]int{"id": id})
}

func updateServer(db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Port         int               `json:"port"`
		MaxPlayers   int               `json:"max_players"`
		Password     string            `json:"password"`
		RconPassword string            `json:"rcon_password"`
		CustomConfig map[string]string `json:"custom_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var password, rconPassword interface{}
	if req.Password != "" {
		password = req.Password
	}
	if req.RconPassword != "" {
		rconPassword = req.RconPassword
	}

	res, err := db.Exec(`
		UPDATE game_servers
		SET port = COALESCE(NULLIF($1, 0), port),
		    max_players = COALESCE(NULLIF($2, 0), max_players),
		    password = $3,
		    rcon_password = $4,
		    updated_at = NOW()
		WHERE id = $5
	`, req.Port, req.MaxPlayers, password, rconPassword, id)
	if err != nil {
		log.Printf("[api] update server error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to update server")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	// Upsert or delete server_configs entries
	for k, v := range req.CustomConfig {
		if v == "" {
			db.Exec(`DELETE FROM server_configs WHERE game_server_id = $1 AND key = $2`, id, k)
		} else {
			_, err := db.Exec(`
				INSERT INTO server_configs (game_server_id, key, value)
				VALUES ($1, $2, $3)
				ON CONFLICT (game_server_id, key) DO UPDATE SET value = EXCLUDED.value
			`, id, k, v)
			if err != nil {
				log.Printf("[api] upsert server_config error for key %s: %v", k, err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func deleteServer(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	var containerName, agentName string
	err := db.QueryRow(`
		SELECT gs.container_name, a.name
		FROM game_servers gs JOIN agents a ON a.id = gs.agent_id
		WHERE gs.id = $1
	`, id).Scan(&containerName, &agentName)

	if err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	// Tell agent to remove container and volume
	if err == nil {
		volumeName := containerName + "-data"
		msg, _ := json.Marshal(Envelope{
			Type: MsgDeleteServer,
			Payload: DeleteServerPayload{
				ServerID:      id,
				ContainerName: containerName,
				VolumeName:    volumeName,
			},
		})
		hub.SendToAgent(agentName, msg)
	}

	db.Exec(`DELETE FROM game_servers WHERE id = $1`, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func startServer(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	payload, agentName, err := buildServerStartPayload(db, id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		log.Printf("[api] start server error: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	msg, _ := json.Marshal(Envelope{
		Type:    MsgStartServer,
		Payload: payload,
	})

	if !hub.SendToAgent(agentName, msg) {
		writeError(w, http.StatusServiceUnavailable, "agent not connected")
		return
	}

	db.Exec(`UPDATE game_servers SET status = 'starting', updated_at = NOW() WHERE id = $1`, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "start command sent"})
}

func stopServer(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	var containerName, agentName string
	err := db.QueryRow(`
		SELECT gs.container_name, a.name
		FROM game_servers gs JOIN agents a ON a.id = gs.agent_id
		WHERE gs.id = $1
	`, id).Scan(&containerName, &agentName)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	msg, _ := json.Marshal(Envelope{
		Type: MsgStopServer,
		Payload: StopServerPayload{
			ServerID:      id,
			ContainerName: containerName,
		},
	})

	if !hub.SendToAgent(agentName, msg) {
		writeError(w, http.StatusServiceUnavailable, "agent not connected")
		return
	}

	db.Exec(`UPDATE game_servers SET status = 'stopping', updated_at = NOW() WHERE id = $1`, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stop command sent"})
}

func sendRcon(hub *Hub, db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	var agentName, rconPassword, containerName string
	var port int
	var fieldMappingsBytes []byte
	err := db.QueryRow(`
		SELECT a.name, gs.port, COALESCE(gs.rcon_password, ''), gs.container_name,
		       COALESCE(gd.field_mappings, '{}')
		FROM game_servers gs
		JOIN agents a ON a.id = gs.agent_id
		JOIN game_definitions gd ON gd.id = gs.game_definition_id
		WHERE gs.id = $1
	`, id).Scan(&agentName, &port, &rconPassword, &containerName, &fieldMappingsBytes)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	var fm GameFieldMappings
	json.Unmarshal(fieldMappingsBytes, &fm)
	rconPort := fm.RconPort
	if rconPort == 0 {
		rconPort = 27015
	}

	messageID := strconv.FormatInt(time.Now().UnixNano(), 10)

	msg, _ := json.Marshal(Envelope{
		Type:      MsgRconCommand,
		MessageID: messageID,
		Payload: RconCommandPayload{
			ServerID:     id,
			MessageID:    messageID,
			Command:      req.Command,
			RconHost:     containerName,
			RconPort:     rconPort,
			RconPassword: rconPassword,
		},
	})

	if !hub.SendToAgent(agentName, msg) {
		writeError(w, http.StatusServiceUnavailable, "agent not connected")
		return
	}

	db.Exec(`INSERT INTO rcon_history (game_server_id, command) VALUES ($1, $2)`, id, req.Command)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "rcon command sent",
		"message_id": messageID,
	})
}

func getServerLogs(db *sql.DB, w http.ResponseWriter, r *http.Request, id int) {
	rows, err := db.Query(`
		SELECT line, logged_at FROM server_log_snapshots
		WHERE game_server_id = $1
		ORDER BY logged_at DESC LIMIT 200
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	defer rows.Close()

	type LogRow struct {
		Line     string    `json:"line"`
		LoggedAt time.Time `json:"logged_at"`
	}

	logs := []LogRow{}
	for rows.Next() {
		var l LogRow
		if err := rows.Scan(&l.Line, &l.LoggedAt); err == nil {
			logs = append(logs, l)
		}
	}

	writeJSON(w, http.StatusOK, logs)
}

func buildServerStartPayload(db *sql.DB, id int) (StartServerPayload, string, error) {
	var s StartServerPayload
	var agentName string
	var gameDefaultEnv, modeConfig, fieldMappingsBytes []byte
	var password, rconPassword sql.NullString

	err := db.QueryRow(`
		SELECT
			gs.id, gs.container_name, gs.port, gs.max_players,
			gs.password, gs.rcon_password,
			gd.docker_image,
			gd.default_env,
			gd.data_path,
			COALESCE(gm.config, '{}') as mode_config,
			COALESCE(gd.field_mappings, '{}') as field_mappings,
			a.name as agent_name
		FROM game_servers gs
		JOIN game_definitions gd ON gd.id = gs.game_definition_id
		LEFT JOIN game_modes gm ON gm.id = gs.game_mode_id
		JOIN agents a ON a.id = gs.agent_id
		WHERE gs.id = $1
	`, id).Scan(
		&s.ServerID, &s.ContainerName, &s.Port, &s.MaxPlayers,
		&password, &rconPassword,
		&s.Image,
		&gameDefaultEnv,
		&s.DataPath,
		&modeConfig,
		&fieldMappingsBytes,
		&agentName,
	)
	if err != nil {
		return s, "", err
	}

	// Fetch port mappings
	portRows, err := db.Query(`
		SELECT label, container_port, protocol, host_port_offset
		FROM game_port_mappings gpm
		JOIN game_definitions gd ON gd.id = gpm.game_definition_id
		JOIN game_servers gs ON gs.game_definition_id = gd.id
		WHERE gs.id = $1
	`, id)
	if err == nil {
		defer portRows.Close()
		for portRows.Next() {
			var pm PortMapping
			if portRows.Scan(&pm.Label, &pm.ContainerPort, &pm.Protocol, &pm.HostPortOffset) == nil {
				s.PortMappings = append(s.PortMappings, pm)
			}
		}
	}

	// Build env vars: game defaults -> mode overrides -> server_configs overrides
	envVars := map[string]string{}

	json.Unmarshal(gameDefaultEnv, &envVars)

	modeEnv := map[string]string{}
	json.Unmarshal(modeConfig, &modeEnv)
	for k, v := range modeEnv {
		envVars[k] = v
	}

	cfgRows, err := db.Query(`
		SELECT key, value FROM server_configs WHERE game_server_id = $1
	`, id)
	if err == nil {
		defer cfgRows.Close()
		for cfgRows.Next() {
			var k, v string
			if cfgRows.Scan(&k, &v) == nil {
				envVars[k] = v
			}
		}
	}

	// Apply server fields using game-specific env var mappings
	var fm GameFieldMappings
	json.Unmarshal(fieldMappingsBytes, &fm)

	if fm.EnvMaxPlayers != "" {
		envVars[fm.EnvMaxPlayers] = strconv.Itoa(s.MaxPlayers)
	}
	if fm.EnvPassword != "" && password.Valid && password.String != "" {
		envVars[fm.EnvPassword] = password.String
	}
	if fm.EnvRconPassword != "" && rconPassword.Valid && rconPassword.String != "" {
		envVars[fm.EnvRconPassword] = rconPassword.String
	}
	for envKey, offset := range fm.PortDerivedVars {
		envVars[envKey] = strconv.Itoa(s.Port + offset)
	}

	s.Env = envVars
	return s, agentName, nil
}
