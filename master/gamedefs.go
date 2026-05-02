package main

import (
	"database/sql"
	"encoding/json"
	"log"
)

type PortMappingDef struct {
	Label          string
	ContainerPort  int
	Protocol       string
	HostPortOffset int
	Description    string
}

type GameModeDef struct {
	Name   string
	Slug   string
	Config map[string]string
}

type GameFieldDef struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	Type        string `json:"type"`
}

type GameDefinition struct {
	Name          string
	Slug          string
	DockerImage   string
	DefaultPort   int
	MaxPlayers    int
	DataPath      string
	DefaultEnv    map[string]string
	FieldMappings GameFieldMappings
	CustomFields  []GameFieldDef
	PortMappings  []PortMappingDef
	Modes         []GameModeDef
}

var registeredGames []GameDefinition

func RegisterGame(g GameDefinition) {
	registeredGames = append(registeredGames, g)
}

func seedGames(db *sql.DB) {
	for _, g := range registeredGames {
		defaultEnvJSON, err := json.Marshal(g.DefaultEnv)
		if err != nil {
			log.Printf("[seedGames] failed to marshal default_env for %s: %v", g.Slug, err)
			continue
		}

		fieldMappingsJSON, err := json.Marshal(g.FieldMappings)
		if err != nil {
			log.Printf("[seedGames] failed to marshal field_mappings for %s: %v", g.Slug, err)
			continue
		}

		customFieldsJSON, err := json.Marshal(g.CustomFields)
		if err != nil {
			log.Printf("[seedGames] failed to marshal custom_fields for %s: %v", g.Slug, err)
			continue
		}

		var gameID int
		err = db.QueryRow(`
			INSERT INTO game_definitions
				(name, slug, docker_image, default_port, default_max_players,
				 default_env, data_path, field_mappings, custom_fields)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (slug) DO UPDATE SET
				docker_image        = EXCLUDED.docker_image,
				default_env         = EXCLUDED.default_env,
				data_path           = EXCLUDED.data_path,
				field_mappings      = EXCLUDED.field_mappings,
				custom_fields       = EXCLUDED.custom_fields,
				default_max_players = EXCLUDED.default_max_players,
				default_port        = EXCLUDED.default_port
			RETURNING id
		`,
			g.Name, g.Slug, g.DockerImage, g.DefaultPort, g.MaxPlayers,
			string(defaultEnvJSON), g.DataPath,
			string(fieldMappingsJSON), string(customFieldsJSON),
		).Scan(&gameID)
		if err != nil {
			log.Printf("[seedGames] upsert game_definitions error for %s: %v", g.Slug, err)
			continue
		}

		// Recreate port mappings
		db.Exec(`DELETE FROM game_port_mappings WHERE game_definition_id = $1`, gameID)
		for _, pm := range g.PortMappings {
			_, err := db.Exec(`
				INSERT INTO game_port_mappings
					(game_definition_id, label, container_port, protocol, host_port_offset, description)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, gameID, pm.Label, pm.ContainerPort, pm.Protocol, pm.HostPortOffset, pm.Description)
			if err != nil {
				log.Printf("[seedGames] insert game_port_mappings error for %s/%s: %v", g.Slug, pm.Label, err)
			}
		}

		// Upsert game modes
		for _, m := range g.Modes {
			configJSON, err := json.Marshal(m.Config)
			if err != nil {
				log.Printf("[seedGames] failed to marshal mode config for %s/%s: %v", g.Slug, m.Slug, err)
				continue
			}
			_, err = db.Exec(`
				INSERT INTO game_modes (game_definition_id, name, slug, config)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (game_definition_id, slug) DO UPDATE SET config = EXCLUDED.config
			`, gameID, m.Name, m.Slug, string(configJSON))
			if err != nil {
				log.Printf("[seedGames] upsert game_modes error for %s/%s: %v", g.Slug, m.Slug, err)
			}
		}

		log.Printf("[seedGames] seeded game: %s (id=%d)", g.Slug, gameID)
	}
}
