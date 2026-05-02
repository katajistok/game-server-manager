# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Build and run (Docker — normal workflow)
```bash
# Start master + UI + DB
docker compose up --build -d

# Start agent only
docker compose -f docker-compose.agent.yml up --build -d

# Rebuild a single service after code changes
docker compose up --build -d master

# Tail logs
docker compose logs -f master
docker compose -f docker-compose.agent.yml logs -f agent
```

### Build Go binaries locally (for quick compile checks)
```bash
cd master && go build ./...
cd agent  && go build ./...
```

### No test suite exists yet. No linter is configured.

---

## Architecture

LanMaster is a three-tier distributed system:

```
Browser ──HTTP/WS──► UI (nginx) ──/api/, /ws/──► Master (Go) ──WS──► Agent (Go)
                                                      │                    │
                                                  PostgreSQL           Docker SDK
```

- **Master** (`master/`) — Go REST API + WebSocket hub. Stores all state in Postgres. Relays messages between agents and the browser UI over WebSocket.
- **Agent** (`agent/`) — runs on the game server host. Connects *outward* to master (no inbound ports needed). Manages Docker containers via the Docker SDK, streams logs, and sends metrics every 30 s.
- **UI** (`ui/`) — vanilla JS single-page app served by nginx. nginx proxies `/api/` and `/ws/` to master on port 8080.

### Message flow

Every agent↔master↔UI interaction goes through WebSocket using typed envelopes (`Envelope{Type, Payload}`). Protocol types are defined in both `master/protocol.go` and `agent/protocol.go` — these are separate copies, kept in sync manually.

Key message types:
- Master → Agent: `start_server`, `stop_server`, `delete_server`, `rcon_command`, `stream_logs_start/stop`
- Agent → Master: `register`, `heartbeat`, `server_status`, `log_line`, `rcon_response`, `metrics`
- Master relays all agent messages to all connected UI clients verbatim.

### Database schema and migrations

`master/db.go` runs migrations on every startup (idempotent `CREATE TABLE IF NOT EXISTS` + `ALTER TABLE ADD COLUMN IF NOT EXISTS`). At the end of migrations, `seedGames()` upserts all registered game definitions — this is safe to run on every boot.

### Game definition system

Games are fully data-driven. Each game is defined by a Go struct (in `master/games_<name>.go`) that self-registers via `init()`. On boot, `seedGames()` upserts the definition into the DB.

- `game_definitions` — docker image, default port, `default_env` (JSON), `field_mappings` (JSON), `custom_fields` (JSON)
- `game_port_mappings` — per-game port → container port mappings
- `game_modes` — named configurations that override `default_env`
- `server_configs` — per-server env var overrides (key = Docker env var name, e.g. `CS2_STARTMAP`)

`field_mappings` maps the 3 universal server fields to game-specific env var names plus RCON config:
```json
{
  "env_max_players":   "CS2_MAXPLAYERS",
  "env_password":      "CS2_PW",
  "env_rcon_password": "CS2_RCONPW",
  "rcon_port":         27015,
  "port_derived_vars": {"TV_PORT": 5}
}
```

`custom_fields` declares the UI form fields for game-specific settings (rendered dynamically — no JS changes per-game):
```json
[
  {"key": "CS2_STARTMAP", "label": "Start Map", "placeholder": "de_dust2", "type": "text"},
  {"key": "CS2_MAPGROUP",  "label": "Map Group",  "placeholder": "mg_active", "type": "text"}
]
```

Env var build order in `buildServerStartPayload`: `default_env` → `game_modes.config` → `server_configs` → `field_mappings` universal fields → `port_derived_vars`.

### JS architecture

`ui/js/` contains ES modules. `app.js` is the entry point and imports all others. Game-specific UI plugins live in `ui/js/games/<slug>.js` and register via `game-registry.js`. Functions needed by HTML `onclick` attributes are assigned to `window` in `app.js`.

### Agent registration flow

1. Agent connects with empty or saved token → master marks it `pending`
2. Admin approves in UI → master generates token, sends `approved` message
3. Agent saves token to `/app/agent.token` → used automatically on reconnects
4. Approved agents bypass the pending flow on reconnect

### RCON

Agent connects directly to the game container by container name (Docker networking) using the Source Engine RCON protocol (`agent/rcon.go`). The RCON port comes from `field_mappings.rcon_port` (falls back to 27015).

### Metrics

Agent sends host + per-container metrics every 30 s. Master keeps a 30-point rolling in-memory history per agent (`Hub.MetricsHistory`). No metrics are persisted to the DB.

---

## Adding a new game

**Go side** — create `master/games_<name>.go`:
```go
func init() {
    RegisterGame(GameDefinition{
        Name: "...", Slug: "...", DockerImage: "...",
        DefaultPort: ..., MaxPlayers: ..., DataPath: "...",
        DefaultEnv:    map[string]string{...},
        FieldMappings: GameFieldMappings{...},
        CustomFields:  []GameFieldDef{
            {Key: "ENV_VAR_NAME", Label: "UI Label", Placeholder: "default", Type: "text"},
        },
        PortMappings: []PortMappingDef{{...}},
        Modes:        []GameModeDef{{...}},
    })
}
```

**JS side** — create `ui/js/games/<name>.js`:
```js
import { registerGame } from '../game-registry.js'
registerGame({ slug: '...', rconPlaceholder: '...' })
```
Then import it in `ui/js/app.js`: `import './games/<name>.js'`

On master boot, `seedGames()` upserts the definition into the DB. `custom_fields` drives the UI form fields dynamically — no other JS/Go changes needed.

---

## Key non-obvious details

- **CS2 always listens on 27015 inside the container** regardless of the host port. Docker port mappings (`host_port_offset` in `game_port_mappings`) handle the external port. Do not set `CS2_PORT` via env.
- **TV_PORT** is derived via `port_derived_vars: {"TV_PORT": 5}` — base port + 5.
- **Agent network**: when agent and master run on the same host, set `AGENT_NETWORK=server_manager_default` so game containers join the same Docker network and the agent can reach them by container name.
- **protocol.go is duplicated** between master and agent. Both must be updated when changing message types.
- **WebSocket pongWait** is set to 120 s on master and agent — these must stay in sync or connections drop every ~60 s.
- **Game data volumes** are named `{container_name}-data` and are only deleted when the server is deleted from the UI, not on stop.
- **Docker SDK version**: the agent uses `github.com/docker/docker v25.0.7` (Go module). The host Docker daemon must support API ≥ 1.44. If you see "client version is too old" errors, upgrade the SDK in `agent/go.mod` and update the `FROM golang:1.25-alpine` line in `agent/Dockerfile` to match the required Go toolchain version.
