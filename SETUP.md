# Game Server Manager — Setup Guide

Game Server Manager is a game server management platform. It consists of a master server, a web UI, a database, and agents that run on game server hosts.

## Architecture Overview

```
Master + UI + Database  <-- runs anywhere (your laptop, a VPS, LAN server)
        |
        | WebSocket
        |
      Agent            <-- runs on the machine that hosts game servers
        |
      Docker           <-- CS2 and other games run as containers
```

---

## Prerequisites

All machines need:
- Docker
- Docker Compose

That is all. No Go, Node.js, or anything else required.

---

## Repository Structure

```
server_manager/
├── master/                  # Go backend API + WebSocket server
│   ├── main.go              # Hub, routing
│   ├── websocket.go         # Agent + UI WebSocket handlers
│   ├── api_servers.go       # Server CRUD + start/stop/rcon
│   ├── api_agents.go        # Agent listing and approval
│   ├── api_games.go         # Game definitions + metrics API
│   ├── api_helpers.go       # Response helpers
│   ├── db.go                # DB init, migrations, seed data
│   ├── gamedefs.go          # Game definition registry
│   ├── games_cs2.go         # CS2 game definition
│   ├── protocol.go          # Message types and payloads
│   ├── go.mod
│   └── Dockerfile
├── agent/                   # Go agent, runs on game server host
│   ├── main.go              # Entry point, reconnect loop
│   ├── agent.go             # WebSocket connection, message handling
│   ├── docker.go            # Docker SDK — container lifecycle, log streaming
│   ├── metrics.go           # Host metrics (gopsutil) + container stats
│   ├── rcon.go              # Source Engine RCON protocol
│   ├── protocol.go          # Message types and payloads (kept in sync with master)
│   ├── go.mod
│   └── Dockerfile
├── ui/                      # Vanilla JS + HTML web interface
│   ├── index.html
│   ├── style.css
│   ├── nginx.conf           # Proxies /api/ and /ws/ to master
│   ├── js/
│   │   ├── app.js           # Entry point, WebSocket, state sync
│   │   ├── state.js         # Global client state
│   │   ├── ws.js            # WebSocket connection
│   │   ├── admin.js         # Admin panel — agent approval, server creation
│   │   ├── agent.js         # Agent panel — metrics visualization
│   │   ├── server.js        # Server panel — logs, RCON, config editing
│   │   ├── sidebar.js       # Navigation
│   │   ├── utils.js         # Utility functions
│   │   ├── game-registry.js # Game UI plugin registry
│   │   └── games/
│   │       └── cs2.js       # CS2 UI plugin
│   └── Dockerfile
├── docker-compose.yml       # Master + UI + Database
├── docker-compose.agent.yml # Agent only
├── SETUP.md                 # This file
├── DESIGN.md                # Architecture reference
└── CLAUDE.md                # Notes for AI-assisted development
```

---

## Part 1 — Setting Up the Master Server

The master server runs the API, web UI, and database. It can run on the same machine as the agent or on a separate machine.

### Step 1 — Clone the repository

```bash
git clone https://github.com/yourusername/server_manager.git
cd server_manager
```

### Step 2 — Configure the master

Open `docker-compose.yml` and change the database password if needed:

```yaml
services:
  db:
    environment:
      - POSTGRES_PASSWORD=password  # Change this to something secret
```

### Step 3 — Start the master

```bash
docker compose up --build -d
```

This starts three containers: `master`, `ui`, and `db`.

### Step 4 — Verify it is running

Open your browser and go to `http://localhost` (or replace localhost with your server IP). You should see the Game Server Manager UI.

---

## Part 2 — Setting Up an Agent

The agent runs on the machine that will host game servers. It connects outward to the master so no inbound firewall rules are needed on the game server host.

### Step 1 — Clone the repository on the game server host

```bash
git clone https://github.com/yourusername/server_manager.git
cd server_manager
```

### Step 2 — Configure the agent

Open `docker-compose.agent.yml` and set these values:

```yaml
services:
  agent:
    environment:
      - MASTER_URL=ws://YOUR_MASTER_IP:8080/ws/agent  # IP of your master server
      - AGENT_NAME=lan-server-1                        # Unique name for this machine
      - AGENT_TOKEN=                                   # Leave empty on first run
```

Replace `YOUR_MASTER_IP` with the IP address of the machine running the master.

If master and agent run on the same machine, use:
```yaml
- MASTER_URL=ws://master:8080/ws/agent
```

### Step 3 — Start the agent

```bash
docker compose -f docker-compose.agent.yml up --build -d
```

### Step 4 — Approve the agent in the UI

1. Open the web UI in your browser
2. Click **⚙ Admin** in the sidebar
3. Under **Pending Agents** you will see your agent waiting
4. Click **✓ Approve**
5. A modal appears with the agent token — click **Copy** to copy it to your clipboard
6. Open `docker-compose.agent.yml` and paste the token:

```yaml
- AGENT_TOKEN=the-token-you-received
```

7. Restart the agent so it uses the token on future reconnects:

```bash
docker compose -f docker-compose.agent.yml restart agent
```

From now on the agent will reconnect automatically using the saved token.

---

## Part 3 — Adding Game Servers

Once an agent is approved you can create game servers from the UI.

1. Open the web UI
2. Click **⚙ Admin** in the sidebar
3. Under **Add Server** fill in the form:
   - **Server Name** — display name, e.g. `CS2 Competitive 1`
   - **Agent** — select the approved agent
   - **Game** — select the game, e.g. `Counter-Strike 2`
   - **Game Mode** — select `Competitive`, `Deathmatch`, or `Casual`
   - **Port** — must be unique per server, e.g. `27015`
   - **Max Players** — e.g. `10` for competitive, `32` for deathmatch
   - **Server Password** — leave empty for a public server
   - **RCON Password** — used for remote console commands
   - **Start Map**, **Map Group**, **Extra Args** — game-specific optional fields
4. Click **➕ Create Server**

Suggested setup for a LAN party:

| Name | Mode | Port | Max Players | Password |
|---|---|---|---|---|
| CS2 Competitive 1 | Competitive | 27015 | 10 | yes |
| CS2 Competitive 2 | Competitive | 27016 | 10 | yes |
| CS2 Deathmatch | Deathmatch | 27017 | 32 | no |
| CS2 Public | Competitive | 27018 | 10 | no |

---

## Part 4 — Managing Servers

### Starting and stopping

1. Click a server in the sidebar
2. Use the **▶ Start** and **■ Stop** buttons in the header
3. Status updates in real time — `stopped` → `starting` → `running`

Note: CS2 downloads about 20 GB on first start. This takes time depending on your internet connection.

### Viewing logs

1. Click a server in the sidebar
2. The **Logs** tab shows live output from the container

### Sending RCON commands

1. Click a server in the sidebar
2. Click the **RCON Console** tab
3. Type a command and press Enter or click **Send**

Useful CS2 RCON commands:
```
status                  show connected players
mp_restartgame 1        restart the match
changelevel de_dust2    change map
mp_maxrounds 16         set max rounds
kick <name>             kick a player
```

### Editing server configuration

1. Click a server in the sidebar
2. Click the **Config** tab
3. Edit any field — max players, port, passwords, start map, etc.
4. Click **Save** to apply on next start, or **Save & Restart** to apply immediately

---

## Part 5 — Running Master and Agent on the Same Machine

If you only have one physical server, you can run everything on it.

Edit `docker-compose.agent.yml`:

```yaml
services:
  agent:
    environment:
      - MASTER_URL=ws://master:8080/ws/agent
```

Then start everything together:

```bash
# Start master + ui + db first
docker compose up --build -d

# Then start the agent
docker compose -f docker-compose.agent.yml up --build -d
```

Both compose files share the `server_manager_default` Docker network so the `master` hostname resolves correctly.

---

## Part 6 — Useful Commands

### View logs

```bash
# Master logs
docker compose logs -f master

# Agent logs
docker compose -f docker-compose.agent.yml logs -f agent
```

### Restart services

```bash
docker compose restart master
docker compose -f docker-compose.agent.yml restart agent
```

### Stop everything

```bash
docker compose down
docker compose -f docker-compose.agent.yml down
```

### Full reset (wipes all data)

```bash
docker compose down -v   # WARNING: deletes all servers, agents, and database
docker compose up -d
docker compose -f docker-compose.agent.yml up -d
```

---

## Adding New Games

To add support for a new game, create two files:

**`master/games_<name>.go`** — game definition:
```go
func init() {
    RegisterGame(GameDefinition{
        Name:        "My Game",
        Slug:        "mygame",
        DockerImage: "image:tag",
        DefaultPort: 1234,
        MaxPlayers:  16,
        DataPath:    "/game/data",
        DefaultEnv:  map[string]string{"ENV_VAR": "value"},
        FieldMappings: GameFieldMappings{
            EnvMaxPlayers:   "GAME_MAXPLAYERS",
            EnvPassword:     "GAME_PASSWORD",
            EnvRconPassword: "GAME_RCON_PASSWORD",
            RconPort:        1234,
        },
        PortMappings: []PortMappingDef{
            {Label: "game", ContainerPort: 1234, Protocol: "both", HostPortOffset: 0},
        },
        CustomFields: []GameFieldDef{
            {Key: "GAME_MAP", Label: "Start Map", Placeholder: "map01", Type: "text"},
        },
    })
}
```

**`ui/js/games/<name>.js`** — UI plugin:
```js
import { registerGame } from '../game-registry.js'
registerGame({ slug: 'mygame', rconPlaceholder: 'say hello' })
```

Then import it in `ui/js/app.js`:
```js
import './games/mygame.js'
```

Rebuild and restart master — the game appears in the UI automatically.

---

## Troubleshooting

**Agent shows as offline after approval**
- Check that `MASTER_URL` has the correct IP address
- Check that port `8080` is open on the master machine
- Check agent logs: `docker compose -f docker-compose.agent.yml logs agent`

**CS2 server stuck on starting**
- The first image pull takes a long time — watch agent logs for download progress
- Make sure the game port is not already in use on the host

**UI shows "Reconnecting..."**
- The master may still be starting up — wait a few seconds and refresh
- Check master logs: `docker compose logs master`

**RCON commands return no response**
- Make sure the server is fully started (status `running`, not `starting`)
- Check that the RCON Password was set when creating the server
- CS2 RCON uses the same port as the game port (e.g. game on `27015` → RCON on `27015`)

**Container metrics not showing**
- The Docker SDK in the agent must support the host daemon API version
- Check agent logs for "client version is too old" errors
- If seen, upgrade the SDK — see `CLAUDE.md` for details

---

## Security Notes

This project is designed primarily for LAN use. If you expose it to the internet:

- Change the default database password in `docker-compose.yml`
- Put the UI and master behind a reverse proxy with HTTPS (e.g. Caddy or nginx)
- Restrict access to ports `8080` and `80` with a firewall
- Use strong RCON passwords on all servers
