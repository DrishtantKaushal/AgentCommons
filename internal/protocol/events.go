package protocol

// Client-to-server message types
const (
	TypeRegister         = "register"
	TypeSubscribe        = "subscribe"
	TypeHeartbeat        = "heartbeat"
	TypeDeregister       = "deregister"
	TypeListAgents       = "list_agents"
	TypeSync             = "sync"
	TypeApprovalRequest  = "approval_request"
	TypeApprovalResponse = "approval_response"
	TypePushMessage      = "push_message"
	TypeListMessages     = "list_messages"
)

// Server-to-client push event types
const (
	TypeRegisterResponse  = "register_response"
	TypeHeartbeatResponse = "heartbeat_response"
	TypeApprovalBroadcast = "approval_broadcast"
	TypeApprovalGranted   = "approval_granted"
	TypeApprovalDenied    = "approval_denied"
	TypeAgentStateChanged  = "agent_state_changed"
	TypeMessagePush        = "message_push"
	TypeServerShuttingDown = "server_shutting_down"
)
