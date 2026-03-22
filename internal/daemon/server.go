package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	clientTypeWrapper    = "wrapper"
	clientTypeSubscriber = "subscriber"
)

// Client represents a connected WebSocket client.
type Client struct {
	Conn       *websocket.Conn
	SessionID  string
	SlotID     string
	ClientType string
	mu         sync.Mutex
}

// Send writes a JSON message to the client.
func (c *Client) Send(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteJSON(msg)
}

// Server is the WebSocket + HTTP server for the daemon.
type Server struct {
	db          *db.DB
	clients     map[string]*Client    // session_id -> client (registered only)
	allConns    map[*Client]bool      // ALL WebSocket connections (for broadcasts)
	subscribers map[string][]*Client  // session_id -> list of subscriber connections (e.g. MCP servers)
	clientsMu   sync.RWMutex
	startedAt   time.Time
	version     string
	connID      int
}

// NewServer creates a new daemon server.
func NewServer(database *db.DB) *Server {
	return &Server{
		db:          database,
		clients:     make(map[string]*Client),
		allConns:    make(map[*Client]bool),
		subscribers: make(map[string][]*Client),
		startedAt:   time.Now(),
		version:     "0.1.0",
	}
}

// ListenAndServe starts the HTTP/WebSocket server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWebSocket)

	log.Printf("[daemon] listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.clientsMu.RLock()
	agentCount := len(s.clients)
	connCount := len(s.allConns)
	s.clientsMu.RUnlock()

	// Quick DB check
	dbOK := true
	if err := s.db.Ping(); err != nil {
		dbOK = false
	}

	resp := map[string]interface{}{
		"status":              "ok",
		"uptime":              time.Since(s.startedAt).Round(time.Second).String(),
		"connected_agents":    agentCount,
		"total_ws_connections": connCount,
		"db_ok":               dbOK,
		"version":             s.version,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[daemon] websocket upgrade failed: %v", err)
		return
	}

	client := &Client{Conn: conn}

	// Track ALL connections for broadcasts (including unregistered MCP servers)
	s.clientsMu.Lock()
	s.allConns[client] = true
	s.clientsMu.Unlock()

	defer func() {
		s.removeClient(client)
		conn.Close()
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[daemon] websocket read error: %v", err)
			}
			return
		}

		var env protocol.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			log.Printf("[daemon] invalid message: %v", err)
			continue
		}

		s.dispatch(client, &env)
	}
}

func (s *Server) dispatch(client *Client, env *protocol.Envelope) {
	switch env.Type {
	case protocol.TypeRegister:
		s.handleRegister(client, env)
	case protocol.TypeSubscribe:
		s.handleSubscribe(client, env)
	case protocol.TypeHeartbeat:
		s.handleHeartbeat(client, env)
	case protocol.TypeDeregister:
		s.handleDeregister(client, env)
	case protocol.TypeListAgents:
		s.handleListAgents(client, env)
	case protocol.TypeApprovalRequest:
		s.handleApprovalRequest(client, env)
	case protocol.TypeApprovalResponse:
		s.handleApprovalResponse(client, env)
	case protocol.TypePushMessage:
		s.handlePushMessage(client, env)
	case protocol.TypeListMessages:
		s.handleListMessages(client, env)
	default:
		log.Printf("[daemon] unknown message type: %s", env.Type)
	}
}

// addClient registers a client by session ID.
func (s *Server) addClient(sessionID, slotID string, client *Client) {
	client.SessionID = sessionID
	client.SlotID = slotID
	s.clientsMu.Lock()
	s.clients[sessionID] = client
	s.clientsMu.Unlock()
}

// removeClient removes a client and handles cleanup.
func (s *Server) removeClient(client *Client) {
	s.clientsMu.Lock()
	delete(s.allConns, client)
	if client.SessionID != "" {
		delete(s.clients, client.SessionID)
	}
	// Remove from subscribers lists
	for sid, subs := range s.subscribers {
		for i, sub := range subs {
			if sub == client {
				s.subscribers[sid] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(s.subscribers[sid]) == 0 {
			delete(s.subscribers, sid)
		}
	}
	s.clientsMu.Unlock()
}

// getClient returns a client by session ID.
func (s *Server) getClient(sessionID string) *Client {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return s.clients[sessionID]
}

// handleSubscribe allows a secondary connection (e.g. MCP server) to subscribe
// to events for a session that was already registered by the wrapper.
func (s *Server) handleSubscribe(client *Client, env *protocol.Envelope) {
	var p protocol.SubscribePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[subscribe] invalid payload: %v", err)
		return
	}

	if p.SessionID == "" {
		log.Printf("[subscribe] empty session_id")
		return
	}

	s.clientsMu.Lock()
	// Set the session ID on the subscribing client so it can be identified
	client.SessionID = p.SessionID
	// Also copy the slot ID from the registered client if available
	if registered, ok := s.clients[p.SessionID]; ok {
		client.SlotID = registered.SlotID
	}
	s.subscribers[p.SessionID] = append(s.subscribers[p.SessionID], client)
	client.ClientType = clientTypeSubscriber
	// Check if there is an active wrapper for this session (while lock is held)
	_, wrapperActive := s.clients[p.SessionID]
	s.clientsMu.Unlock()

	log.Printf("[subscribe] client subscribed to session %s (wrapper_active=%v)", p.SessionID, wrapperActive)

	// Acknowledge the subscription
	s.sendResponse(client, "subscribe_response", env.RequestID, map[string]interface{}{
		"status":         "subscribed",
		"session_id":     p.SessionID,
		"wrapper_active": wrapperActive,
	})
}

// sendToSession sends a message to the registered client AND all subscribers
// for the given session_id. This enables targeted delivery instead of broadcast.
func (s *Server) sendToSession(sessionID string, env *protocol.Envelope) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("[sendToSession] marshal error: %v", err)
		return
	}

	sent := 0

	// Send to the registered client (wrapper)
	if client, ok := s.clients[sessionID]; ok {
		client.mu.Lock()
		err := client.Conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()
		if err != nil {
			log.Printf("[sendToSession] send to registered client failed: %v", err)
		} else {
			sent++
		}
	}

	// Send to all subscribers (e.g. MCP servers)
	if subs, ok := s.subscribers[sessionID]; ok {
		for _, sub := range subs {
			sub.mu.Lock()
			err := sub.Conn.WriteMessage(websocket.TextMessage, data)
			sub.mu.Unlock()
			if err != nil {
				log.Printf("[sendToSession] send to subscriber failed: %v", err)
			} else {
				sent++
			}
		}
	}

	if sent == 0 {
		log.Printf("[sendToSession] no clients found for session %s", sessionID)
	}
}

// sendToWrapperOnly sends a message ONLY to the registered wrapper client
// for the given session_id, skipping all subscribers. This is used for
// routing approval_granted/denied exclusively to the wrapper.
func (s *Server) sendToWrapperOnly(sessionID string, env *protocol.Envelope) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("[sendToWrapperOnly] marshal error: %v", err)
		return
	}

	client, ok := s.clients[sessionID]
	if !ok {
		log.Printf("[sendToWrapperOnly] no wrapper client found for session %s", sessionID)
		return
	}

	client.mu.Lock()
	err = client.Conn.WriteMessage(websocket.TextMessage, data)
	client.mu.Unlock()
	if err != nil {
		log.Printf("[sendToWrapperOnly] send failed: %v", err)
	}
}

// broadcast sends a message to ALL connected WebSocket clients except the excluded one.
// This includes unregistered connections (like MCP servers that couldn't register
// because the wrapper already claimed the slot).
// Subscribers whose session has an active wrapper are also skipped to prevent
// MCP subscribers from receiving approval broadcasts redundantly.
func (s *Server) broadcast(env *protocol.Envelope, excludeSessionID string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("[daemon] broadcast marshal error: %v", err)
		return
	}

	for c := range s.allConns {
		if c.SessionID == excludeSessionID {
			continue
		}
		// Skip MCP subscribers whose session has an active wrapper —
		// the wrapper already handles notifications for that session.
		if c.ClientType == clientTypeSubscriber && s.clients[c.SessionID] != nil {
			continue
		}
		c.mu.Lock()
		err := c.Conn.WriteMessage(websocket.TextMessage, data)
		c.mu.Unlock()
		if err != nil {
			log.Printf("[daemon] broadcast failed: %v", err)
		}
	}
}

// sendResponse sends a typed response back to a client.
func (s *Server) sendResponse(client *Client, msgType, requestID string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[daemon] marshal response: %v", err)
		return
	}
	env := protocol.Envelope{
		Type:      msgType,
		RequestID: requestID,
		Payload:   data,
	}
	if err := client.Send(env); err != nil {
		log.Printf("[daemon] send response: %v", err)
	}
}

// DB returns the database handle (used by registry, heartbeat, etc.).
func (s *Server) DB() *db.DB { return s.db }

// StartedAt returns the server start time.
func (s *Server) StartedAt() time.Time { return s.startedAt }

// Addr returns the listen address string for the configured port.
func Addr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}
