package protocol

import "encoding/json"

// Envelope is the top-level wire format for all WebSocket messages.
type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// --- Client-to-Server Payloads ---

type RegisterPayload struct {
	AgentType        string `json:"agent_type"`
	TerminalName     string `json:"terminal_name"`
	PID              int    `json:"pid"`
	CWD              string `json:"cwd"`
	RepoRoot         string `json:"repo_root,omitempty"`
	ClaudeSessionID  string `json:"claude_session_id,omitempty"`
}

type HeartbeatPayload struct {
	SessionID   string `json:"session_id"`
	State       string `json:"state"`
	StateDetail string `json:"state_detail,omitempty"`
	CWD         string `json:"cwd,omitempty"`
}

type SubscribePayload struct {
	SessionID string `json:"session_id"`
}

type DeregisterPayload struct {
	SessionID string `json:"session_id"`
}

type ListAgentsPayload struct {
	Filter string `json:"filter,omitempty"` // "alive" | "all"
}

type SyncPayload struct {
	SessionID string `json:"session_id"`
	Since     string `json:"since"`
}

type ApprovalRequestPayload struct {
	SessionID  string `json:"session_id"`
	SlotID     string `json:"slot_id"`
	PromptText string `json:"prompt_text"`
	PromptHash string `json:"prompt_hash"`
	Context    string `json:"context,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

type ApprovalResponsePayload struct {
	TargetSlotName string `json:"target_slot_name"`
	RequestID      string `json:"request_id"`
	Action         string `json:"action"` // "approve" | "deny"
	PromptHash     string `json:"prompt_hash"`
}

// --- Server-to-Client Payloads ---

type RegisterResponse struct {
	SessionID     string       `json:"session_id"`
	SlotID        string       `json:"slot_id"`
	IsNewSlot     bool         `json:"is_new_slot"`
	ServerVersion string       `json:"server_version"`
	SlotContext   *SlotContext `json:"slot_context,omitempty"`
	Error         string       `json:"error,omitempty"`
	Message       string       `json:"message,omitempty"`
}

type SlotContext struct {
	PreviousSessions int                `json:"previous_sessions"`
	LastSession      *LastSessionInfo   `json:"last_session,omitempty"`
	PendingMessages  []PendingMessage   `json:"pending_messages,omitempty"`
	MissedEvents     []json.RawMessage  `json:"missed_events,omitempty"`
}

type LastSessionInfo struct {
	EndedAt         string `json:"ended_at"`
	EndReason       string `json:"end_reason"`
	LastCWD         string `json:"last_cwd"`
	LastBranch      string `json:"last_branch,omitempty"`
	LastStateDetail string `json:"last_state_detail,omitempty"`
}

type PendingMessage struct {
	ID       string `json:"id"`
	FromSlot string `json:"from_slot"`
	Content  string `json:"content"`
	SentAt   string `json:"sent_at"`
}

type HeartbeatResponse struct {
	Ack                  bool `json:"ack"`
	PendingNotifications int  `json:"pending_notifications"`
}

type ApprovalBroadcast struct {
	RequestID   string `json:"request_id"`
	SessionID   string `json:"session_id"`
	SlotID      string `json:"slot_id"`
	SlotName    string `json:"slot_name"`
	PromptText  string `json:"prompt_text"`
	PromptHash  string `json:"prompt_hash"`
	Context     string `json:"context,omitempty"`
	RequestedAt string `json:"requested_at"`
	TTLSeconds  int    `json:"ttl_seconds,omitempty"`
}

type ApprovalGranted struct {
	RequestID  string `json:"request_id"`
	PromptHash string `json:"prompt_hash"`
	ApprovedBy string `json:"approved_by"`
	ApprovedAt string `json:"approved_at"`
}

type ApprovalDenied struct {
	RequestID  string `json:"request_id"`
	PromptHash string `json:"prompt_hash"`
	DeniedBy   string `json:"denied_by"`
	DeniedAt   string `json:"denied_at"`
}

type AgentStateChanged struct {
	SessionID string `json:"session_id"`
	SlotName  string `json:"slot_name"`
	OldState  string `json:"old_state"`
	NewState  string `json:"new_state"`
	Detail    string `json:"detail,omitempty"`
}

type PushMessagePayload struct {
	TargetSlotName string `json:"target_slot_name"`
	Content        string `json:"content"`
	MessageType    string `json:"message_type"` // "message" or "task"
}

type MessagePush struct {
	FromSlotName string `json:"from_slot_name"`
	Content      string `json:"content"`
	MessageType  string `json:"message_type"` // "message" or "task"
	SentAt       string `json:"sent_at"`
}

type ServerShuttingDown struct {
	Reason        string `json:"reason"`
	GracePeriodMs int    `json:"grace_period_ms"`
}

// --- Query/Response types ---

type SlotInfo struct {
	SlotID            string       `json:"slot_id"`
	SlotName          string       `json:"slot_name"`
	CreatedAt         string       `json:"created_at"`
	LastState         string       `json:"last_state"`
	LastStateDetail   string       `json:"last_state_detail,omitempty"`
	LastCWD           string       `json:"last_cwd,omitempty"`
	TotalSessions     int          `json:"total_sessions"`
	ActiveSession     *SessionInfo `json:"active_session,omitempty"`
}

type SessionInfo struct {
	SessionID     string `json:"session_id"`
	AgentType     string `json:"agent_type"`
	PID           int    `json:"pid"`
	CWD           string `json:"cwd"`
	State         string `json:"state"`
	StateDetail   string `json:"state_detail,omitempty"`
	StartedAt     string `json:"started_at"`
	LastHeartbeat string `json:"last_heartbeat"`
}

type ListAgentsResponse struct {
	Agents []SlotInfo `json:"agents"`
}

type SyncResponse struct {
	Events []json.RawMessage `json:"events"`
}

// --- Message History ---

type ListMessagesPayload struct {
	Limit int `json:"limit,omitempty"`
}

type MessageHistoryItem struct {
	ID        string `json:"id"`
	FromName  string `json:"from_name"`
	ToName    string `json:"to_name"`
	Content   string `json:"content"`
	Type      string `json:"type"`      // "message" or "task"
	Status    string `json:"status"`    // "delivered" or "pending"
	CreatedAt string `json:"created_at"`
}

type ListMessagesResponse struct {
	Messages []MessageHistoryItem `json:"messages"`
}
