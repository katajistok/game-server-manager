package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

func handleGameDefinitions(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
		SELECT
			gd.id, gd.name, gd.slug, gd.default_port, gd.default_max_players,
			gd.default_env, COALESCE(gd.field_mappings, '{}'), COALESCE(gd.custom_fields, '[]'),
			gm.id as mode_id, gm.name as mode_name, gm.slug as mode_slug
		FROM game_definitions gd
		LEFT JOIN game_modes gm ON gm.game_definition_id = gd.id
		ORDER BY gd.name, gm.name
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type ModeOption struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	type GameDef struct {
		ID                int             `json:"id"`
		Name              string          `json:"name"`
		Slug              string          `json:"slug"`
		DefaultPort       int             `json:"default_port"`
		DefaultMaxPlayers int             `json:"default_max_players"`
		DefaultEnv        json.RawMessage `json:"default_env"`
		FieldMappings     json.RawMessage `json:"field_mappings"`
		CustomFields      json.RawMessage `json:"custom_fields"`
		Modes             []ModeOption    `json:"modes"`
	}

	defsMap := map[int]*GameDef{}
	defOrder := []int{}

	for rows.Next() {
		var gID, gDefaultPort, gDefaultMax int
		var gName, gSlug string
		var gDefaultEnv, gFieldMappings, gCustomFields []byte
		var mID *int
		var mName, mSlug *string

		if err := rows.Scan(
			&gID, &gName, &gSlug, &gDefaultPort, &gDefaultMax,
			&gDefaultEnv, &gFieldMappings, &gCustomFields,
			&mID, &mName, &mSlug,
		); err != nil {
			continue
		}

		if _, ok := defsMap[gID]; !ok {
			defsMap[gID] = &GameDef{
				ID:                gID,
				Name:              gName,
				Slug:              gSlug,
				DefaultPort:       gDefaultPort,
				DefaultMaxPlayers: gDefaultMax,
				DefaultEnv:        gDefaultEnv,
				FieldMappings:     gFieldMappings,
				CustomFields:      gCustomFields,
				Modes:             []ModeOption{},
			}
			defOrder = append(defOrder, gID)
		}

		if mID != nil {
			defsMap[gID].Modes = append(defsMap[gID].Modes, ModeOption{
				ID: *mID, Name: *mName, Slug: *mSlug,
			})
		}
	}

	result := make([]*GameDef, 0, len(defOrder))
	for _, id := range defOrder {
		result = append(result, defsMap[id])
	}

	writeJSON(w, http.StatusOK, result)
}

func handleMetrics(hub *Hub, w http.ResponseWriter, r *http.Request) {
	// /api/metrics/agent-name
	agentName := strings.TrimPrefix(r.URL.Path, "/api/metrics/")
	if agentName == "" {
		writeError(w, http.StatusBadRequest, "missing agent name")
		return
	}
	history := hub.GetMetricsHistory(agentName)
	writeJSON(w, http.StatusOK, history)
}
