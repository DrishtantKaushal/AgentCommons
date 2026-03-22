package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Slot Operations ---

type Slot struct {
	SlotID              string
	SlotName            string
	MachineID           string
	UserID              string
	CreatedAt           string
	LastCWD             sql.NullString
	LastRepoRoot        sql.NullString
	LastBranch          sql.NullString
	LastState           string
	LastStateDetail     sql.NullString
	LastSessionEndedAt  sql.NullString
	CurrentSessionID    sql.NullString
	TotalSessions       int
	TotalDurationSeconds int
}

// GetSlotByID looks up a slot by its primary key.
func (db *DB) GetSlotByID(slotID string) (*Slot, error) {
	s := &Slot{}
	err := db.QueryRow(`
		SELECT slot_id, slot_name, machine_id, user_id, created_at,
		       last_cwd, last_repo_root, last_branch, last_state, last_state_detail,
		       last_session_ended_at, current_session_id, total_sessions, total_duration_seconds
		FROM agent_slots
		WHERE slot_id = ?`,
		slotID,
	).Scan(
		&s.SlotID, &s.SlotName, &s.MachineID, &s.UserID, &s.CreatedAt,
		&s.LastCWD, &s.LastRepoRoot, &s.LastBranch, &s.LastState, &s.LastStateDetail,
		&s.LastSessionEndedAt, &s.CurrentSessionID, &s.TotalSessions, &s.TotalDurationSeconds,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// FindSlotByName looks up a slot by (machine_id, slot_name).
func (db *DB) FindSlotByName(machinID, slotName string) (*Slot, error) {
	s := &Slot{}
	err := db.QueryRow(`
		SELECT slot_id, slot_name, machine_id, user_id, created_at,
		       last_cwd, last_repo_root, last_branch, last_state, last_state_detail,
		       last_session_ended_at, current_session_id, total_sessions, total_duration_seconds
		FROM agent_slots
		WHERE machine_id = ? AND slot_name = ?`,
		machinID, slotName,
	).Scan(
		&s.SlotID, &s.SlotName, &s.MachineID, &s.UserID, &s.CreatedAt,
		&s.LastCWD, &s.LastRepoRoot, &s.LastBranch, &s.LastState, &s.LastStateDetail,
		&s.LastSessionEndedAt, &s.CurrentSessionID, &s.TotalSessions, &s.TotalDurationSeconds,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// InsertSlot creates a new persistent slot.
func (db *DB) InsertSlot(slotName, machineID, userID string) (*Slot, error) {
	slotID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO agent_slots (slot_id, slot_name, machine_id, user_id, created_at, last_state)
		VALUES (?, ?, ?, ?, ?, 'active')`,
		slotID, slotName, machineID, userID, now,
	)
	if err != nil {
		return nil, err
	}
	return &Slot{
		SlotID:    slotID,
		SlotName:  slotName,
		MachineID: machineID,
		UserID:    userID,
		CreatedAt: now,
		LastState: "active",
	}, nil
}

// UpdateSlotSession binds or unbinds a session to a slot.
func (db *DB) UpdateSlotSession(slotID, sessionID string) error {
	_, err := db.Exec(`
		UPDATE agent_slots SET current_session_id = ?, last_state = 'active'
		WHERE slot_id = ?`,
		sessionID, slotID,
	)
	return err
}

// ClearSlotSession unbinds the current session and snapshots state.
func (db *DB) ClearSlotSession(slotID, lastCWD, lastBranch, lastState, lastStateDetail string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE agent_slots
		SET current_session_id = NULL,
		    last_state = ?,
		    last_state_detail = ?,
		    last_cwd = ?,
		    last_branch = ?,
		    last_session_ended_at = ?
		WHERE slot_id = ?`,
		lastState, lastStateDetail, lastCWD, lastBranch, now, slotID,
	)
	return err
}

// IncrementSlotSessions bumps total_sessions count.
func (db *DB) IncrementSlotSessions(slotID string) error {
	_, err := db.Exec(`
		UPDATE agent_slots SET total_sessions = total_sessions + 1 WHERE slot_id = ?`,
		slotID,
	)
	return err
}

// --- Session Operations ---

type Session struct {
	SessionID       string
	SlotID          string
	AgentType       string
	PID             int
	CWD             string
	RepoRoot        sql.NullString
	ClaudeSessionID sql.NullString
	State           string
	StateDetail     sql.NullString
	StartedAt       string
	LastHeartbeat   string
	LastUserInput   sql.NullString
	EndedAt         sql.NullString
	EndReason       sql.NullString
	IsAlive         int
}

// InsertSession creates a new ephemeral session under a slot.
func (db *DB) InsertSession(slotID, agentType string, pid int, cwd, repoRoot, claudeSessionID string) (*Session, error) {
	sessionID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	var repoRootVal, claudeVal interface{}
	if repoRoot != "" {
		repoRootVal = repoRoot
	}
	if claudeSessionID != "" {
		claudeVal = claudeSessionID
	}

	_, err := db.Exec(`
		INSERT INTO agent_sessions (session_id, slot_id, agent_type, pid, cwd, repo_root, claude_session_id, started_at, last_heartbeat)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, slotID, agentType, pid, cwd, repoRootVal, claudeVal, now, now,
	)
	if err != nil {
		return nil, err
	}
	return &Session{
		SessionID:     sessionID,
		SlotID:        slotID,
		AgentType:     agentType,
		PID:           pid,
		CWD:           cwd,
		State:         "idle",
		StartedAt:     now,
		LastHeartbeat: now,
		IsAlive:       1,
	}, nil
}

// UpdateSessionHeartbeat updates heartbeat fields on a session.
func (db *DB) UpdateSessionHeartbeat(sessionID, state, stateDetail, cwd string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE agent_sessions
		SET last_heartbeat = ?, state = ?, state_detail = ?, cwd = COALESCE(?, cwd)
		WHERE session_id = ?`,
		now, state, stateDetail, nilIfEmpty(cwd), sessionID,
	)
	return err
}

// MarkSessionDead marks a session as dead with the given reason.
func (db *DB) MarkSessionDead(sessionID, endReason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE agent_sessions
		SET is_alive = 0, state = 'dead', ended_at = ?, end_reason = ?
		WHERE session_id = ?`,
		now, endReason, sessionID,
	)
	return err
}

// GetSession returns a session by ID.
func (db *DB) GetSession(sessionID string) (*Session, error) {
	s := &Session{}
	err := db.QueryRow(`
		SELECT session_id, slot_id, agent_type, pid, cwd, repo_root, claude_session_id,
		       state, state_detail, started_at, last_heartbeat, last_user_input,
		       ended_at, end_reason, is_alive
		FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(
		&s.SessionID, &s.SlotID, &s.AgentType, &s.PID, &s.CWD, &s.RepoRoot, &s.ClaudeSessionID,
		&s.State, &s.StateDetail, &s.StartedAt, &s.LastHeartbeat, &s.LastUserInput,
		&s.EndedAt, &s.EndReason, &s.IsAlive,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

// ListStaleSessions returns alive sessions whose last heartbeat is older than the cutoff.
func (db *DB) ListStaleSessions(cutoff time.Time) ([]Session, error) {
	rows, err := db.Query(`
		SELECT session_id, slot_id, agent_type, pid, cwd, repo_root, claude_session_id,
		       state, state_detail, started_at, last_heartbeat, last_user_input,
		       ended_at, end_reason, is_alive
		FROM agent_sessions
		WHERE is_alive = 1 AND last_heartbeat < ?`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(
			&s.SessionID, &s.SlotID, &s.AgentType, &s.PID, &s.CWD, &s.RepoRoot, &s.ClaudeSessionID,
			&s.State, &s.StateDetail, &s.StartedAt, &s.LastHeartbeat, &s.LastUserInput,
			&s.EndedAt, &s.EndReason, &s.IsAlive,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// --- Slot + Session join queries ---

type SlotWithSession struct {
	Slot
	Session *Session
}

// ListSlotsWithSessions returns all slots with their active session (if any).
func (db *DB) ListSlotsWithSessions(machineID string, aliveOnly bool) ([]SlotWithSession, error) {
	query := `
		SELECT s.slot_id, s.slot_name, s.machine_id, s.user_id, s.created_at,
		       s.last_cwd, s.last_repo_root, s.last_branch, s.last_state, s.last_state_detail,
		       s.last_session_ended_at, s.current_session_id, s.total_sessions, s.total_duration_seconds,
		       sess.session_id, sess.agent_type, sess.pid, sess.cwd, sess.state, sess.state_detail,
		       sess.started_at, sess.last_heartbeat
		FROM agent_slots s
		LEFT JOIN agent_sessions sess ON s.current_session_id = sess.session_id
		WHERE s.machine_id = ?`

	if aliveOnly {
		query += ` AND s.current_session_id IS NOT NULL`
	}

	query += ` ORDER BY s.created_at`

	rows, err := db.Query(query, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SlotWithSession
	for rows.Next() {
		var sw SlotWithSession
		var sessID, sessType, sessCWD, sessState, sessDetail, sessStarted, sessHB sql.NullString
		var sessPID sql.NullInt64

		if err := rows.Scan(
			&sw.SlotID, &sw.SlotName, &sw.MachineID, &sw.UserID, &sw.CreatedAt,
			&sw.LastCWD, &sw.LastRepoRoot, &sw.LastBranch, &sw.LastState, &sw.LastStateDetail,
			&sw.LastSessionEndedAt, &sw.CurrentSessionID, &sw.TotalSessions, &sw.TotalDurationSeconds,
			&sessID, &sessType, &sessPID, &sessCWD, &sessState, &sessDetail,
			&sessStarted, &sessHB,
		); err != nil {
			return nil, err
		}

		if sessID.Valid {
			sw.Session = &Session{
				SessionID:     sessID.String,
				AgentType:     sessType.String,
				PID:           int(sessPID.Int64),
				CWD:           sessCWD.String,
				State:         sessState.String,
				StartedAt:     sessStarted.String,
				LastHeartbeat: sessHB.String,
			}
			if sessDetail.Valid {
				sw.Session.StateDetail = sessDetail
			}
		}

		results = append(results, sw)
	}
	return results, rows.Err()
}

// --- Message Operations ---

// InsertMessage stores a message.
func (db *DB) InsertMessage(fromSlotID, fromSessionID, toSlotID, toSessionID, msgType, content, metadata string, expiresAt *time.Time) (string, error) {
	id := uuid.New().String()
	var exp interface{}
	if expiresAt != nil {
		exp = expiresAt.UTC().Format(time.RFC3339)
	}

	_, err := db.Exec(`
		INSERT INTO messages (id, from_slot_id, from_session_id, to_slot_id, to_session_id, type, content, metadata, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nilIfEmpty(fromSlotID), nilIfEmpty(fromSessionID),
		nilIfEmpty(toSlotID), nilIfEmpty(toSessionID),
		msgType, content, nilIfEmpty(metadata), exp,
	)
	return id, err
}

// GetPendingMessages returns pending messages for a slot.
func (db *DB) GetPendingMessages(slotID string) ([]Message, error) {
	rows, err := db.Query(`
		SELECT m.id, m.from_slot_id, m.content, m.metadata, m.created_at,
		       COALESCE(s.slot_name, '') as from_slot_name
		FROM messages m
		LEFT JOIN agent_slots s ON m.from_slot_id = s.slot_id
		WHERE m.to_slot_id = ? AND m.status = 'pending'
		ORDER BY m.created_at`,
		slotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromSlotID, &m.Content, &m.Metadata, &m.CreatedAt, &m.FromSlotName); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MarkMessagesDelivered marks messages as delivered.
func (db *DB) MarkMessagesDelivered(ids []string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE messages SET status = 'delivered', delivered_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
	}
	return nil
}

type Message struct {
	ID           string
	FromSlotID   sql.NullString
	FromSlotName string
	Content      string
	Metadata     sql.NullString
	CreatedAt    string
}

// --- Presence Log ---

// AppendPresenceLog records a state transition.
func (db *DB) AppendPresenceLog(sessionID, slotID, oldState, newState, detail, requestID string) error {
	_, err := db.Exec(`
		INSERT INTO presence_log (session_id, slot_id, old_state, new_state, detail, request_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, slotID, oldState, newState, nilIfEmpty(detail), nilIfEmpty(requestID),
	)
	return err
}

// --- Message History ---

// MessageWithNames represents a message with resolved slot names.
type MessageWithNames struct {
	ID        string
	FromName  string
	ToName    string
	Content   string
	MsgType   string // "message" or "task" (extracted from metadata)
	Status    string // "pending" or "delivered"
	CreatedAt string
}

// ListMessages returns recent messages for the machine with slot names resolved via JOIN.
func (db *DB) ListMessages(machineID string, limit int) ([]MessageWithNames, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := db.Query(`
		SELECT m.id,
		       COALESCE(fs.slot_name, 'cli') AS from_name,
		       COALESCE(ts.slot_name, 'unknown') AS to_name,
		       m.content,
		       COALESCE(m.metadata, '') AS metadata,
		       m.status,
		       m.created_at
		FROM messages m
		LEFT JOIN agent_slots fs ON m.from_slot_id = fs.slot_id
		LEFT JOIN agent_slots ts ON m.to_slot_id = ts.slot_id
		WHERE (fs.machine_id = ? OR ts.machine_id = ?)
		ORDER BY m.created_at DESC
		LIMIT ?`,
		machineID, machineID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []MessageWithNames
	for rows.Next() {
		var m MessageWithNames
		var metadata string
		if err := rows.Scan(&m.ID, &m.FromName, &m.ToName, &m.Content, &metadata, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}

		// Extract message_type from metadata JSON: {"message_type":"task"}
		m.MsgType = "message"
		if strings.Contains(metadata, `"task"`) {
			m.MsgType = "task"
		}

		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
