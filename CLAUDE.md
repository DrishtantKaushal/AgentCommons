# AgentCommons -- Build Instructions

AgentCommons is a communication layer for orchestrating agent activity across sessions, terminals, providers, accounts, and machines. An attention multiplexer. R1 is the first release: two Claude Code terminals on one machine that can see each other, relay approvals without switching, and maintain persistent identity across session restarts.

## What R1 delivers

- `/status` in Terminal 1 shows both terminals, their state, and cwd
- When Terminal 2 blocks on an approval prompt, Terminal 1 gets a notification
- `/approve @ZohoMetrics` in Terminal 1 injects the keystroke into Terminal 2 via pty
- `/push @ZohoMetrics "message"` sends a message to Terminal 2 (direct push by default, `--mailbox` alternative)
- `/inbox` lists pending mailbox messages; `/read @name` imports a message into context
- Dual notification modes: direct push (default, real-time) and mailbox (zero context cost, pull on demand)
- Close Terminal 2, reopen it later, slot reconnects automatically, pending messages deliver
- Machine stays awake via caffeinate while agents run
- Daemon auto-launches on first connection (no manual start)
- Terminal names auto-assigned (from Cursor, from context, or from a wordlist)

## Tech stack

- **Go** -- daemon, session wrapper, CLI. Single binary: `commons`. Uses `cobra` for CLI, `creack/pty` for pty, `gorilla/websocket` for WebSocket, `modernc.org/sqlite` for SQLite (pure Go, no CGO).
- **TypeScript** -- MCP server spawned by Claude Code via stdio. Uses `@modelcontextprotocol/sdk` and `ws`.
- **SQLite + WAL** -- `~/.commons/commons.db`. WAL mode for concurrent reads.
- **WebSocket** -- JSON over WebSocket on `localhost:7390` between daemon, MCP servers, and wrappers.

## Project structure

```
├── CLAUDE.md                          # This file
├── go.mod
├── go.sum
├── main.go                            # CLI entry point (cobra root command)
├── cmd/
│   ├── server.go                      # commons server start/stop/status
│   ├── run.go                         # commons run claude (wrapper entry)
│   ├── status.go                      # commons status (query daemon)
│   ├── approve.go                     # commons approve @name
│   ├── deny.go                        # commons deny @name
│   ├── install.go                     # commons install (one-time setup)
│   └── mcp_server.go                  # commons mcp-server (execs node)
├── internal/
│   ├── daemon/
│   │   ├── daemon.go                  # Main daemon process lifecycle
│   │   ├── server.go                  # WebSocket server + /health endpoint
│   │   ├── registry.go                # Slot creation/claiming + session registration
│   │   ├── heartbeat.go               # Heartbeat processing + reaper goroutine
│   │   ├── approval.go                # Approval request broadcast + response routing
│   │   ├── messages.go                # Message storage + slot-addressed queuing
│   │   └── bootstrap.go               # Bootstrap payload generation on slot reclaim
│   ├── db/
│   │   ├── db.go                      # SQLite init, WAL config, migrations
│   │   ├── schema.go                  # CREATE TABLE statements as Go constants
│   │   └── queries.go                 # Prepared query functions
│   ├── wrapper/
│   │   ├── wrapper.go                 # commons run claude orchestration
│   │   ├── pty.go                     # Pty allocation, output/input proxy loops
│   │   ├── detector.go                # Approval pattern regex scanner
│   │   ├── injector.go                # Approval/denial keystroke injection
│   │   └── caffeinate.go              # macOS caffeinate / Linux systemd-inhibit
│   ├── naming/
│   │   ├── naming.go                  # Terminal name resolution (3-layer)
│   │   ├── cursor.go                  # Cursor terminal title detection
│   │   ├── context.go                 # Context-based name generation
│   │   └── wordlist.go                # Memorable wordlist fallback
│   ├── protocol/
│   │   ├── messages.go                # All message type structs (JSON-serializable)
│   │   └── events.go                  # WebSocket event type constants
│   └── config/
│       └── config.go                  # config.toml parsing + defaults
├── mcp/                               # TypeScript MCP server
│   ├── package.json
│   ├── tsconfig.json
│   ├── src/
│   │   ├── index.ts                   # MCP server entry point (stdio transport)
│   │   ├── tools.ts                   # MCP tool definitions (status, push, inbox, read, report_state)
│   │   ├── daemon-client.ts           # WebSocket client + auto-launch + reconnection
│   │   ├── bootstrap.ts               # Bootstrap payload processing on slot reclaim
│   │   └── channels.ts               # Channel protocol for direct push notifications
│   └── approval-patterns.yaml        # Regex patterns for Claude Code approval prompts
└── docs/
    └── approval-patterns.md           # How to add patterns for new CLI tools
```

## Build commands

```bash
# Go daemon + CLI + wrapper
go mod tidy
go build -o commons .

# TypeScript MCP server
cd mcp
npm install
npm run build
```

The `commons` binary handles everything: daemon, CLI, wrapper, and MCP server invocation. The MCP server is a separate TypeScript process that `commons mcp-server` execs via `node`.

## How to run locally

### 1. Install (one time)

```bash
go build -o commons .
cp commons /usr/local/bin/   # or add repo root to PATH
commons install
```

This creates `~/.commons/`, initializes the SQLite database, writes config files, and registers the MCP server in `~/.claude/settings.local.json`. From this point forward, every new Claude Code session automatically connects to the commons via MCP. The daemon auto-launches on first connection. No special commands needed.

### 2. Normal usage (every day)

Just open Claude Code normally. The MCP server auto-registers the session with the daemon. `/status` and `/inbox` work immediately. Terminal names are auto-assigned (see "Terminal naming" below).

```bash
# Terminal 1 -- just open Claude Code
claude

# Terminal 2 -- just open Claude Code
claude
```

Both sessions register with the commons via MCP. Both show up in `/status`. The daemon auto-launches if not already running.

### 3. Usage with approval relay

To enable cross-terminal approval injection (the pty wrapper that intercepts approval prompts and injects keystrokes), use `commons run claude` instead of `claude`:

```bash
# Terminal 1
commons run claude

# Terminal 2
commons run claude
```

The wrapper auto-launches the daemon (first time), auto-assigns a terminal name, allocates a pty, and launches Claude Code inside it. You see one line at the top confirming registration, then normal Claude Code.

The `--name` flag is optional -- use it only if you want to override the auto-assigned name:

```bash
commons run claude --name AuthWork
```

### 4. Test the approval flow

In Terminal 1 (inside Claude Code): type `/status`. You should see both agents.

In Terminal 2: ask Claude to run a bash command. When the approval prompt appears, Terminal 1's MCP server receives the broadcast.

In Terminal 1: type `/approve @ZohoMetrics`. Terminal 2 resumes.

### 5. Test slot persistence

Close Terminal 2. Run `commons status` -- the slot shows as inactive (persists). Reopen Terminal 2 with `commons run claude`. If the auto-detected name matches the previous slot, it reconnects to the existing slot, session #2. You can also explicitly reconnect: `commons run claude --name ZohoMetrics`.

## The Daemon

The daemon is a Go background process that coordinates all commons activity on the machine.

### What it does

- Listens on `localhost:7390` for WebSocket connections from MCP servers and session wrappers
- Manages persistent slots (agent identities that survive session restarts)
- Routes approval events between terminals
- Stores messages in SQLite for offline delivery
- Tracks agent liveness via heartbeats and PID checks
- Exposes `GET /health` for readiness checks

### How it starts

The daemon auto-launches when the first MCP server or session wrapper connects and discovers no daemon running. It spawns as a detached background process. The user never needs to run `commons server start` manually.

On auto-launch, the connecting client polls `/health` for up to 6 seconds until the daemon is ready. If the daemon crashes, the next MCP server connection auto-relaunches it.

### Where data lives

```
~/.commons/
├── commons.db                # SQLite database (WAL mode)
├── config.toml               # Daemon configuration (port, heartbeat intervals)
├── approval-patterns.yaml    # Regex patterns for approval prompt detection
└── daemon.pid                # PID file for the running daemon
```

All persistent state is in SQLite. The daemon is stateless except as a cache of what the database contains. If the daemon crashes and restarts, slots and messages survive.

### How to manage it

| Command | What it does |
|---------|-------------|
| `commons server start` | Manually start the daemon (rarely needed -- auto-launches) |
| `commons server stop` | Graceful shutdown: notifies all clients, flushes WAL, exits |
| `commons server status` | Reports health, uptime, connected agent count, database size |

Example:
```
$ commons server status
Commons daemon: running (pid 45123, uptime 2h34m)
Connected agents: 2
Database: ~/.commons/commons.db (2.1 MB)
```

### Auto-restart behavior

If the daemon crashes while agents are running:
1. MCP servers and wrappers detect the broken WebSocket connection
2. They enter fallback mode (no crash, wrapper continues as pass-through)
3. On next reconnection attempt, they auto-relaunch the daemon
4. After restart, a 60-second grace period prevents false disconnections while clients reconnect

## Terminal naming

Terminal names are auto-assigned. No `--name` flag required. Three layers of resolution, in priority order:

### Layer 1: Cursor terminal title

If the user named their terminal in Cursor (e.g., right-click tab, "Rename"), the MCP server reads the Cursor terminal title and uses it as the slot name. Cursor stores this in `terminal.log` and `state.vscdb`. This is the primary source -- if you name your terminals in Cursor, the commons picks it up automatically.

### Layer 2: Context-based generation

If no Cursor name is detected, generate a name from available context:
- **From first user prompt:** The MCP server sees the first user prompt and extracts the key topic. "Build a rate limiter" becomes "RateLimiter". "Set up Zoho CRM integration" becomes "ZohoCRM". Simple NLP extraction -- take the most distinctive noun phrase and PascalCase it.
- **From cwd basename:** If no prompt is available yet, use the working directory basename. `~/work/backend-api` becomes "BackendAPI". `~/repos/auth-service` becomes "AuthService".
- **Combined:** If both are available, the prompt-derived name takes priority since it is more specific than the directory name.

### Layer 3: Memorable wordlist fallback

If nothing else works, pick from a curated `adjective + noun` wordlist. Contextual-sounding names like "SwiftFalcon", "QuietForge", "BrightForge" -- modeled on Claude Code's session names ("rosy-waddling-dusk"), Docker's container names ("festive-fermat"), and MCP Agent Mail's pattern ("GreenCastle"). The wordlist is curated to avoid ambiguity and to feel like real project names rather than random UUIDs.

### Renaming

The user can always rename a terminal after assignment:
- Inside Claude Code: `/communeAlias MyCustomName`
- From any shell: `commons slot rename <old> <new>`

### Override

`--name` still works as an optional override on `commons run claude`:
```bash
commons run claude --name AuthWork
```

This bypasses all auto-detection and uses "AuthWork" directly.

## Key architecture decisions

### Persistent slots

Slots and sessions are separate layers. A slot is a persistent identity ("ZohoMetrics"). A session is an ephemeral process. Messages address slots, not sessions. When a session dies, the slot survives. When a new session claims the slot, queued messages deliver.

Schema hierarchy: User > Machine > Slot (persistent) > Session (ephemeral) > Connection (WebSocket).

One slot per name per machine. `UNIQUE(machine_id, slot_name)`. Simultaneous same-name registration is rejected.

### Two integration paths

1. **MCP server only (default).** Every Claude Code session auto-connects via MCP. Provides `/status`, `/inbox`, presence tracking, and message delivery. No pty wrapper, no approval injection.

2. **MCP server + pty wrapper (`commons run claude`).** Adds approval detection and remote keystroke injection on top of the MCP integration. Use this when you want cross-terminal approval relay.

### Notification model

**Direct push (default):** Notifications stored in MCP server memory, surfaced on next `commons_inbox` call. Zero context cost.

**Channel protocol (optional):** If Claude Code launched with `--dangerously-load-development-channels`, push via `notifications/claude/channel` for real-time interrupts.

**Always-direct:** System messages (approval requests/responses, errors) always push immediately regardless of config.

### Mailbox storage

All messages stored in SQLite with dual addressing: `to_slot_id` (persistent, survives sessions) and `to_session_id` (ephemeral). Messages to inactive slots queue in `pending` status, delivered via bootstrap payload on next session claim.

### Caffeinate

`commons run claude` spawns `caffeinate -i -w $PID` on macOS to prevent idle sleep. Tied to wrapper PID -- exits automatically when wrapper exits. On Linux: `systemd-inhibit`.

### Auto-launch daemon

MCP server and wrapper both auto-launch the daemon as a detached process if connection is refused. Poll `/health` for up to 6s. No manual `commons server start` needed.

### WebSocket protocol

JSON over WebSocket. Every message has `type`, `request_id`, `payload`. Types: `register`, `heartbeat`, `deregister`, `list_agents`, `sync`, `approval_request`, `approval_response`. Server push events: `approval_broadcast`, `approval_granted`, `agent_state_changed`, `server_shutting_down`.

### Approval detection and injection

The session wrapper scans Claude Code stdout with compiled regexes on a rolling line buffer. On match: capture prompt text, compute SHA-256 hash, publish `approval_request` to daemon. On `approval_granted`: verify hash matches current pending prompt, write `y` to pty master fd.

### SQLite schema (core tables)

- `users` -- single row for MVP
- `machines` -- single row for MVP
- `agent_slots` -- persistent identity, slot_name unique per machine
- `agent_sessions` -- ephemeral, one per CLI process lifetime
- `messages` -- slot-addressed with type enum (approval_request, approval_response, direct, broadcast, system)
- `presence_log` -- append-only state transitions

### Heartbeat and liveness

Wrapper heartbeats every 10s. Reaper scans every 10s for sessions with `last_heartbeat` > 30s old. PID liveness check supplements: if PID dead, mark disconnected immediately. 60-second grace period after daemon restart before marking unreconnected sessions.
