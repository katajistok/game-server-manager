package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

func initDB() (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://lanmaster:password@localhost:5432/lanmaster?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	for i := 0; i < 10; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		log.Printf("[db] Waiting for database... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, err
	}

	if err := runMigrations(db); err != nil {
		return nil, err
	}

	return db, nil
}

func runMigrations(db *sql.DB) error {
	log.Printf("[db] Running migrations...")

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id          SERIAL PRIMARY KEY,
			username    VARCHAR(64) UNIQUE NOT NULL,
			password    VARCHAR(255) NOT NULL,
			role        VARCHAR(32) NOT NULL DEFAULT 'admin',
			created_at  TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS agents (
			id           SERIAL PRIMARY KEY,
			name         VARCHAR(64) UNIQUE NOT NULL,
			token        VARCHAR(255) NOT NULL,
			last_seen_at TIMESTAMP,
			status       VARCHAR(32) NOT NULL DEFAULT 'offline',
			host_info    JSONB,
			created_at   TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS game_definitions (
			id                  SERIAL PRIMARY KEY,
			name                VARCHAR(64) UNIQUE NOT NULL,
			slug                VARCHAR(32) UNIQUE NOT NULL,
			docker_image        VARCHAR(255) NOT NULL,
			default_port        INTEGER NOT NULL,
			default_max_players INTEGER NOT NULL,
			config_file_path    VARCHAR(255),
			default_env         JSONB NOT NULL DEFAULT '{}',
			data_path           VARCHAR(255) NOT NULL DEFAULT '/data',
			created_at          TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS game_port_mappings (
			id                  SERIAL PRIMARY KEY,
			game_definition_id  INTEGER NOT NULL REFERENCES game_definitions(id) ON DELETE CASCADE,
			label               VARCHAR(32) NOT NULL,
			container_port      INTEGER NOT NULL,
			protocol            VARCHAR(4) NOT NULL DEFAULT 'udp',
			host_port_offset    INTEGER NOT NULL DEFAULT 0,
			description         VARCHAR(128)
		)`,

		`CREATE TABLE IF NOT EXISTS game_modes (
			id                  SERIAL PRIMARY KEY,
			game_definition_id  INTEGER NOT NULL REFERENCES game_definitions(id),
			name                VARCHAR(64) NOT NULL,
			slug                VARCHAR(32) NOT NULL,
			config              JSONB NOT NULL DEFAULT '{}',
			UNIQUE(game_definition_id, slug)
		)`,

		`CREATE TABLE IF NOT EXISTS game_servers (
			id                  SERIAL PRIMARY KEY,
			agent_id            INTEGER NOT NULL REFERENCES agents(id),
			game_definition_id  INTEGER NOT NULL REFERENCES game_definitions(id),
			game_mode_id        INTEGER REFERENCES game_modes(id),
			name                VARCHAR(64) NOT NULL,
			container_id        VARCHAR(128),
			container_name      VARCHAR(128) NOT NULL,
			port                INTEGER NOT NULL,
			max_players         INTEGER NOT NULL,
			password            VARCHAR(255),
			rcon_password       VARCHAR(255),
			status              VARCHAR(32) NOT NULL DEFAULT 'stopped',
			created_at          TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at          TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS server_configs (
			id              SERIAL PRIMARY KEY,
			game_server_id  INTEGER NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
			key             VARCHAR(128) NOT NULL,
			value           TEXT,
			is_secret       BOOLEAN NOT NULL DEFAULT FALSE,
			UNIQUE(game_server_id, key)
		)`,

		`CREATE TABLE IF NOT EXISTS rcon_history (
			id              SERIAL PRIMARY KEY,
			game_server_id  INTEGER NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
			user_id         INTEGER REFERENCES users(id),
			command         TEXT NOT NULL,
			response        TEXT,
			sent_at         TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS server_log_snapshots (
			id              SERIAL PRIMARY KEY,
			game_server_id  INTEGER NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
			line            TEXT NOT NULL,
			logged_at       TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS events (
			id              SERIAL PRIMARY KEY,
			user_id         INTEGER REFERENCES users(id),
			agent_id        INTEGER REFERENCES agents(id),
			game_server_id  INTEGER REFERENCES game_servers(id),
			type            VARCHAR(64) NOT NULL,
			payload         JSONB,
			created_at      TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// Alter existing databases to add new columns
		`ALTER TABLE game_definitions ADD COLUMN IF NOT EXISTS default_env JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE game_definitions ADD COLUMN IF NOT EXISTS data_path VARCHAR(255) NOT NULL DEFAULT '/data'`,
		`ALTER TABLE game_definitions ADD COLUMN IF NOT EXISTS field_mappings JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE game_definitions ADD COLUMN IF NOT EXISTS custom_fields JSONB NOT NULL DEFAULT '[]'`,

		// Migrate game-specific columns from game_servers into server_configs (only if they exist)
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='game_servers' AND column_name='start_map') THEN
				INSERT INTO server_configs (game_server_id, key, value)
					SELECT id, 'CS2_STARTMAP', start_map FROM game_servers
					WHERE start_map IS NOT NULL AND start_map != ''
					ON CONFLICT (game_server_id, key) DO NOTHING;
			END IF;
		END $$`,

		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='game_servers' AND column_name='map_group') THEN
				INSERT INTO server_configs (game_server_id, key, value)
					SELECT id, 'CS2_MAPGROUP', map_group FROM game_servers
					WHERE map_group IS NOT NULL AND map_group != ''
					ON CONFLICT (game_server_id, key) DO NOTHING;
			END IF;
		END $$`,

		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='game_servers' AND column_name='extra_args') THEN
				INSERT INTO server_configs (game_server_id, key, value)
					SELECT id, 'CS2_ADDITIONAL_ARGS', extra_args FROM game_servers
					WHERE extra_args IS NOT NULL AND extra_args != ''
					ON CONFLICT (game_server_id, key) DO NOTHING;
			END IF;
		END $$`,

		`ALTER TABLE game_servers DROP COLUMN IF EXISTS start_map`,
		`ALTER TABLE game_servers DROP COLUMN IF EXISTS map_group`,
		`ALTER TABLE game_servers DROP COLUMN IF EXISTS extra_args`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return err
		}
	}

	seedGames(db)
	log.Printf("[db] Migrations complete")
	return nil
}

// --- Agent DB helpers ---

func upsertPendingAgent(db *sql.DB, name string, info HostInfo) error {
	infoJSON, _ := json.Marshal(info)
	_, err := db.Exec(`
		INSERT INTO agents (name, token, status, host_info, last_seen_at)
		VALUES ($1, '', 'pending', $2, NOW())
		ON CONFLICT (name) DO UPDATE
			SET host_info    = $2,
			    last_seen_at = NOW(),
			    status       = CASE
			    	WHEN agents.status = 'pending' THEN 'pending'
			    	ELSE agents.status
			    END
	`, name, infoJSON)
	return err
}

func approveAgent(db *sql.DB, id int) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)
	_, err := db.Exec(`
		UPDATE agents SET token = $1, status = 'approved' WHERE id = $2
	`, token, id)
	return token, err
}

func validateAgentToken(db *sql.DB, name, token string) bool {
	var stored string
	err := db.QueryRow(
		`SELECT token FROM agents WHERE name = $1`, name,
	).Scan(&stored)
	if err != nil {
		return false
	}
	return stored == token
}

func updateAgentStatus(db *sql.DB, name, status string, info HostInfo) {
	infoJSON, _ := json.Marshal(info)
	db.Exec(`
		UPDATE agents
		SET status = $1, last_seen_at = NOW(), host_info = $2
		WHERE name = $3
	`, status, infoJSON, name)
}

func updateAgentLastSeen(db *sql.DB, name string) {
	db.Exec(`UPDATE agents SET last_seen_at = NOW() WHERE name = $1`, name)
}
