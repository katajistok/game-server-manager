# Game Server Manager — Design Document

## Overview

Game server management platform for LAN parties and home labs. Master + UI + DB run together; agents run on game server hosts. Fully containerized — everything runs via Docker Compose.

## Architecture

```
Browser ──HTTP/WS──► UI (nginx) ──/api/, /ws/──► Master (Go) ──WS──► Agent (Go)
                                                      │                    │
                                                  PostgreSQL           Docker SDK
```

- **master** (Go) — REST API + WebSocket relay. Stores all state in Postgres. Routes messages between agents and the browser UI.
- **ui** (Vanilla JS + nginx) — single-page app. nginx proxies `/api/` and `/ws/` to master on port 8080.
- **db** (Postgres) — master only. Swappable for a managed DB service.
- **agent** (Go) — runs on the game server host. Connects outward to master via WebSocket. Manages Docker containers, streams logs, collects host and container metrics.

## Repository Structure

```
server_manager/
├── master/
│   ├── main.go          # Hub setup, HTTP routing
│   ├── websocket.go     # Agent + UI WebSocket handlers
│   ├── api_servers.go   # Server CRUD + start/stop/rcon
│   ├── api_agents.go    # Agent listing and approval
│   ├── api_games.go     # Game definitions + metrics API
│   ├── api_helpers.go   # Response helpers
│   ├── db.go            # DB init, migrations, seed data, DB helpers
│   ├── gamedefs.go      # GameDefinition struct + RegisterGame() + seedGames()
│   ├── games_cs2.go     # CS2 game definition (add games_<name>.go for new games)
│   ├── protocol.go      # Message types and payload structs
│   └── Dockerfile
├── agent/
│   ├── main.go          # Entry point, reconnect loop
│   ├── agent.go         # WebSocket connection, message routing, server lifecycle
│   ├── docker.go        # Docker SDK — container start/stop/delete, log streaming
│   ├── metrics.go       # Host metrics (gopsutil) + container stats (Docker Stats API)
│   ├── rcon.go          # Source Engine RCON binary protocol
│   ├── protocol.go      # Message types (kept in sync with master/protocol.go manually)
│   └── Dockerfile
├── ui/
│   ├── index.html
│   ├── style.css
│   ├── nginx.conf
│   └── js/
│       ├── app.js           # Entry point — WebSocket, state sync, global functions
│       ├── state.js         # Global client state object
│       ├── ws.js            # WebSocket connection + reconnect
│       ├── admin.js         # Admin panel — agent approval, server creation
│       ├── agent.js         # Agent panel — host metrics, sparklines
│       ├── server.js        # Server panel — logs, RCON, config editing
│       ├── sidebar.js       # Sidebar navigation and listings
│       ├── utils.js         # Utility functions
│       ├── game-registry.js # Game UI plugin registry
│       └── games/
│           └── cs2.js       # CS2 UI plugin (RCON placeholder)
├── docker-compose.yml        # master + ui + db
├── docker-compose.agent.yml  # agent only
├── SETUP.md
├── DESIGN.md
└── CLAUDE.md
```

## Tech Stack

- Go 1.22 (master), Go 1.25 (agent — required by Docker SDK v25)
- Vanilla JS, no framework (ui)
- PostgreSQL 16
- `github.com/docker/docker v25.0.7` — Docker SDK for agent (requires host daemon API ≥ 1.44)
- `github.com/gorilla/websocket v1.5.1`
- `github.com/shirou/gopsutil/v3` — host metrics
- nginx:alpine (ui container)

## WebSocket Protocol

Every agent↔master↔UI interaction uses typed JSON envelopes:

```go
Envelope{ Type, MessageID, Payload }
```

Message flow:
```
Master → Agent:   start_server, stop_server, delete_server,
                  rcon_command, stream_logs_start, stream_logs_stop, approved

Agent → Master:   register, heartbeat, server_status, log_line,
                  rcon_response, metrics

Master → UI:      server_status, log_line, rcon_response, metrics
```

`protocol.go` is duplicated between master and agent — both files must be updated together when changing message types.

## Agent Registration Flow

1. Agent starts with empty or stored token
2. Agent connects and sends `register` with AgentName + Token + HostInfo
3. If unknown or pending → master puts agent in pending state
4. Admin opens UI → Admin panel → clicks Approve
5. Master generates token, sends `approved` to waiting agent
6. Agent saves token to `/app/agent.token`
7. Admin copies token from UI modal → pastes into `docker-compose.agent.yml` as `AGENT_TOKEN`
8. Future reconnects use saved token for instant authentication

## Game Definition System

Games are fully data-driven. Each game is defined by a Go struct that self-registers via `init()` in `master/games_<name>.go`. On boot, `seedGames()` upserts all registered definitions into the database.

`GameDefinition` fields:
- `DockerImage`, `DefaultPort`, `MaxPlayers`, `DataPath`
- `DefaultEnv` — base environment variables for the container
- `FieldMappings` — maps universal fields (max_players, password, rcon_password) to game-specific env var names
- `CustomFields` — dynamic UI form fields (rendered without JS changes)
- `PortMappings` — which container ports to expose to the host
- `Modes` — named configurations that override `DefaultEnv` (e.g. competitive, deathmatch)

Env var build order in `buildServerStartPayload`:
```
default_env → game_modes.config → server_configs → field_mappings → port_derived_vars
```

## Database Schema

```
users                login credentials (not yet enforced)
agents               registered agents — name, token, status, host_info (JSONB)
game_definitions     game templates — docker_image, default_port, default_env (JSONB),
                       field_mappings (JSONB), custom_fields (JSONB), data_path
game_port_mappings   per-game port→container port mappings
game_modes           named env overrides per game (competitive, deathmatch, etc.)
game_servers         server instances — agent, game, mode, port, passwords, status
server_configs       per-server key-value env var overrides (replaces game-specific columns)
rcon_history         RCON command log
server_log_snapshots cached log lines (not yet persisted from live stream)
events               audit log
```

Migrations run on every boot (idempotent `CREATE TABLE IF NOT EXISTS` + `ALTER TABLE ADD COLUMN IF NOT EXISTS`).

## Game Server Lifecycle

1. Admin creates server (Admin → Add Server) — stored in DB with status `stopped`
2. Admin clicks Start → master sends `start_server` payload to agent via WebSocket
3. Agent creates named volume, pulls Docker image, starts container
4. Agent sends `server_status: running` back to master
5. Master updates DB, relays status to UI
6. On Stop: agent stops container, sends `server_status: stopped`
7. On Delete: agent stops + removes container AND volume, master removes from DB

## Container Naming and Volumes

- Containers named `lanmaster-{sanitized-server-name}`
- Data volume: `{container_name}-data`
- Volume persists game files across restarts — no re-download on stop/start
- Volume only deleted when server is deleted from the UI
- Mount path comes from `game_definitions.data_path` (e.g. `/home/steam/cs2-dedicated` for CS2)

## Metrics

- Agent collects host metrics every 30 s via gopsutil: CPU %, RAM used/total, Disk used/total
- Agent collects per-container metrics via Docker Stats API: CPU %, RAM used/limit
- Metrics flow: Agent → master (30-point rolling in-memory history) → UI via WebSocket
- UI displays sparkline charts (canvas) on agent panel and server Metrics tab
- HostInfo (OS, CPU cores, RAM GB, Disk GB) is sent once at registration and stored in DB

## Network Configuration

- Agent and game containers must share a Docker network
- Set `AGENT_NETWORK=server_manager_default` in `docker-compose.agent.yml`
- Agent mounts `/proc` and `/sys` from host for accurate host-level metrics
- CS2 RCON: agent connects to the game container by name on the shared Docker network

## Adding New Games

Create two files:

**`master/games_<name>.go`:**
```go
func init() {
    RegisterGame(GameDefinition{
        Name: "My Game", Slug: "mygame", DockerImage: "image:tag",
        DefaultPort: 1234, MaxPlayers: 16, DataPath: "/game/data",
        DefaultEnv:    map[string]string{"ENV_VAR": "value"},
        FieldMappings: GameFieldMappings{
            EnvMaxPlayers:   "GAME_MAXPLAYERS",
            EnvPassword:     "GAME_PASSWORD",
            EnvRconPassword: "GAME_RCON_PASSWORD",
            RconPort:        1234,
        },
        CustomFields: []GameFieldDef{
            {Key: "GAME_MAP", Label: "Start Map", Placeholder: "map01", Type: "text"},
        },
        PortMappings: []PortMappingDef{
            {Label: "game", ContainerPort: 1234, Protocol: "both", HostPortOffset: 0},
        },
    })
}
```

**`ui/js/games/<name>.js`:**
```js
import { registerGame } from '../game-registry.js'
registerGame({ slug: 'mygame', rconPlaceholder: 'say hello' })
```

Import it in `ui/js/app.js`:
```js
import './games/mygame.js'
```

Rebuild master — the game appears in the UI automatically.

## Known Limitations

- **No UI authentication** — any LAN user can access the UI and manage servers
- **Logs not persisted** — `server_log_snapshots` table exists but live log lines are not written to it
- **In-memory metrics only** — metrics history is lost on master restart (last 30 points per agent)
- **protocol.go is duplicated** — master and agent each have a copy; must be kept in sync manually
