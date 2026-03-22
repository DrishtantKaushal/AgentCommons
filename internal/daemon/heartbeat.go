package daemon

import (
	"encoding/json"
	"log"
	"syscall"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
)

func (s *Server) handleHeartbeat(client *Client, env *protocol.Envelope) {
	var p protocol.HeartbeatPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[heartbeat] invalid payload: %v", err)
		return
	}

	if err := s.db.UpdateSessionHeartbeat(p.SessionID, p.State, p.StateDetail, p.CWD); err != nil {
		log.Printf("[heartbeat] update error: %v", err)
	}

	// Count pending notifications for this session
	pending := 0 // TODO: count pending messages for the session's slot

	s.sendResponse(client, protocol.TypeHeartbeatResponse, env.RequestID, protocol.HeartbeatResponse{
		Ack:                  true,
		PendingNotifications: pending,
	})
}

// StartReaper runs the heartbeat reaper goroutine.
func (s *Server) StartReaper(interval, timeout, gracePeriod time.Duration, stop <-chan struct{}) {
	go func() {
		// Wait for grace period after daemon startup
		select {
		case <-time.After(gracePeriod):
		case <-stop:
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.reapStaleSessions(timeout)
			case <-stop:
				return
			}
		}
	}()
}

func (s *Server) reapStaleSessions(timeout time.Duration) {
	cutoff := time.Now().Add(-timeout)
	sessions, err := s.db.ListStaleSessions(cutoff)
	if err != nil {
		log.Printf("[reaper] list stale sessions error: %v", err)
		return
	}

	for _, sess := range sessions {
		// PID liveness check: if PID is dead, mark immediately
		if !isPIDAlive(sess.PID) {
			log.Printf("[reaper] PID %d dead for session %s, marking disconnected", sess.PID, sess.SessionID)
		} else {
			log.Printf("[reaper] session %s heartbeat stale (pid %d alive), marking disconnected", sess.SessionID, sess.PID)
		}

		// Mark session dead
		if err := s.db.MarkSessionDead(sess.SessionID, "heartbeat_timeout"); err != nil {
			log.Printf("[reaper] mark dead error: %v", err)
			continue
		}

		// Clear slot session
		if err := s.db.ClearSlotSession(sess.SlotID, sess.CWD, "", "inactive", ""); err != nil {
			log.Printf("[reaper] clear slot error: %v", err)
		}

		// Log presence transition
		oldState := sess.State
		s.db.AppendPresenceLog(sess.SessionID, sess.SlotID, oldState, "disconnected", "heartbeat_timeout", "")

		// Remove the WebSocket client if still registered.
		// Also remove from allConns so future broadcasts don't write to a
		// closed connection (previously this was a leak — bug-009).
		s.clientsMu.Lock()
		if c, ok := s.clients[sess.SessionID]; ok {
			c.Conn.Close()
			delete(s.clients, sess.SessionID)
			delete(s.allConns, c)
		}
		s.clientsMu.Unlock()

		// Look up slot name for broadcast so other terminals see which slot timed out
		slotName := ""
		if slot, err := s.db.GetSlotByID(sess.SlotID); err == nil && slot != nil {
			slotName = slot.SlotName
		}

		// Broadcast state change
		s.broadcastStateChange(sess.SessionID, slotName, oldState, "disconnected", "heartbeat_timeout")
	}
}

// isPIDAlive checks if a process is alive via kill(pid, 0).
func isPIDAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
