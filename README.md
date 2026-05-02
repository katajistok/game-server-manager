# Game Server Manager

A self-hosted game server management platform built for LAN parties. Deploy and manage game servers (CS2, and more) through a web UI — no command line needed after setup.

![Architecture](https://img.shields.io/badge/Go-master%20%2B%20agent-blue) ![UI](https://img.shields.io/badge/UI-Vanilla%20JS-yellow) ![DB](https://img.shields.io/badge/DB-PostgreSQL-blue)

## Features

- **Web UI** — start, stop, and monitor game servers from a browser
- **Live logs** — stream container output in real time
- **RCON console** — send commands to running servers directly from the UI
- **Server config editing** — change passwords, max players, maps without touching the command line
- **Metrics** — CPU, RAM, and disk usage for the host and each game container
- **Multi-game support** — add new games by dropping in a single Go file
- **Agent-based** — agents run on game server hosts and connect outward; no inbound firewall rules needed

## Architecture

```
Master + UI + DB  ◄── WebSocket ──►  Agent  ◄──►  Docker (game containers)
```

- **Master** — Go REST API + WebSocket hub, stores state in PostgreSQL
- **Agent** — Go process on the game server host, manages Docker containers
- **UI** — Vanilla JS SPA served by nginx

## Quick Start

### Prerequisites

- Docker + Docker Compose on all machines. Nothing else required.

### 1. Start the master server

```bash
git clone https://github.com/katajistok/game-server-manager.git
cd game-server-manager
docker compose up -d
```

Open `http://localhost` in your browser.

### 2. Start the agent (on the game server host)

```bash
git clone https://github.com/katajistok/game-server-manager.git
cd game-server-manager

# Edit docker-compose.agent.yml and set:
#   MASTER_URL=ws://<master-ip>:8080/ws/agent
#   AGENT_NAME=my-server

docker compose -f docker-compose.agent.yml up -d
```

> If master and agent run on the same machine, use `MASTER_URL=ws://master:8080/ws/agent`.

### 3. Approve the agent

1. Open the UI → click **⚙ Admin**
2. Under **Pending Agents**, click **✓ Approve**
3. Copy the token from the dialog that appears
4. Paste it as `AGENT_TOKEN` in `docker-compose.agent.yml`
5. Restart the agent: `docker compose -f docker-compose.agent.yml restart agent`

### 4. Create a server

1. UI → **⚙ Admin** → **Add Server**
2. Fill in name, game, port, passwords
3. Click **➕ Create Server**
4. Click the server in the sidebar → **▶ Start**

> CS2 downloads ~20 GB on first start — this takes a while.

## Adding New Games

Create `master/games_<name>.go` with a `RegisterGame()` call and `ui/js/games/<name>.js` for the UI plugin. See `DESIGN.md` for the full example and field reference.

## Documentation

- [SETUP.md](SETUP.md) — full setup guide with all options and troubleshooting
- [DESIGN.md](DESIGN.md) — architecture, database schema, and developer reference

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Master | Go 1.22, gorilla/websocket, lib/pq |
| Agent | Go 1.25, Docker SDK v25, gopsutil |
| UI | Vanilla JS, nginx |
| Database | PostgreSQL 16 |
