package db

// Schema contains all CREATE TABLE and CREATE INDEX statements for the commons database.
const Schema = `
-- Enable WAL mode and pragmas
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

-- Single implicit user for MVP
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Single implicit machine for MVP
CREATE TABLE IF NOT EXISTS machines (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    machine_name TEXT NOT NULL,
    hardware_id TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Persistent agent slots (survive across sessions)
CREATE TABLE IF NOT EXISTS agent_slots (
    slot_id TEXT PRIMARY KEY,
    slot_name TEXT NOT NULL,
    machine_id TEXT NOT NULL REFERENCES machines(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),

    -- Last known state (snapshot from most recent session)
    last_cwd TEXT,
    last_repo_root TEXT,
    last_branch TEXT,
    last_state TEXT DEFAULT 'inactive',
    last_state_detail TEXT,
    last_session_ended_at TEXT,

    -- Current session binding (null if no active session)
    current_session_id TEXT,

    -- Cumulative stats
    total_sessions INTEGER NOT NULL DEFAULT 0,
    total_duration_seconds INTEGER NOT NULL DEFAULT 0,

    UNIQUE(machine_id, slot_name)
);

-- Ephemeral agent sessions (one per CLI process lifetime)
CREATE TABLE IF NOT EXISTS agent_sessions (
    session_id TEXT PRIMARY KEY,
    slot_id TEXT NOT NULL REFERENCES agent_slots(slot_id),
    agent_type TEXT NOT NULL DEFAULT 'claude-code',
    pid INTEGER NOT NULL,
    cwd TEXT NOT NULL,
    repo_root TEXT,

    -- Claude Code session linkage
    claude_session_id TEXT,

    state TEXT NOT NULL DEFAULT 'idle'
        CHECK (state IN ('working','idle','blocked_on_approval','error','disconnected','dead')),
    state_detail TEXT,

    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_heartbeat TEXT NOT NULL DEFAULT (datetime('now')),
    last_user_input TEXT,
    ended_at TEXT,
    end_reason TEXT,

    is_alive INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_sessions_slot ON agent_sessions(slot_id, is_alive);
CREATE INDEX IF NOT EXISTS idx_sessions_alive ON agent_sessions(is_alive);
CREATE INDEX IF NOT EXISTS idx_slots_machine ON agent_slots(machine_id);
CREATE INDEX IF NOT EXISTS idx_slots_active ON agent_slots(current_session_id) WHERE current_session_id IS NOT NULL;

-- Messages (slot-addressed for persistence, session-addressed for ephemeral)
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    from_slot_id TEXT REFERENCES agent_slots(slot_id),
    from_session_id TEXT REFERENCES agent_sessions(session_id),
    to_slot_id TEXT REFERENCES agent_slots(slot_id),
    to_session_id TEXT REFERENCES agent_sessions(session_id),
    type TEXT NOT NULL
        CHECK (type IN ('approval_request','approval_response','direct','broadcast','system')),
    content TEXT NOT NULL,
    metadata TEXT,
    in_reply_to TEXT REFERENCES messages(id),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','delivered','expired')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    delivered_at TEXT,
    expires_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_messages_type ON messages(type, status);
CREATE INDEX IF NOT EXISTS idx_messages_slot ON messages(to_slot_id, status);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(to_session_id, status);

-- Presence log (append-only, for debugging)
CREATE TABLE IF NOT EXISTS presence_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES agent_sessions(session_id),
    slot_id TEXT NOT NULL REFERENCES agent_slots(slot_id),
    old_state TEXT NOT NULL,
    new_state TEXT NOT NULL,
    detail TEXT,
    request_id TEXT,
    timestamp TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_presence_session ON presence_log(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_presence_slot ON presence_log(slot_id, timestamp);
`
