package daemon

import (
	"encoding/json"
	"log"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
)

func (s *Server) handleApprovalRequest(client *Client, env *protocol.Envelope) {
	var p protocol.ApprovalRequestPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[approval] invalid request payload: %v", err)
		return
	}

	// Compute expiry
	var expiresAt *time.Time
	if p.TTLSeconds > 0 {
		t := time.Now().Add(time.Duration(p.TTLSeconds) * time.Second)
		expiresAt = &t
	}

	// Store the approval request as a message
	metadata, _ := json.Marshal(map[string]string{
		"prompt_hash": p.PromptHash,
		"context":     p.Context,
	})

	msgID, err := s.db.InsertMessage(
		client.SlotID, p.SessionID,
		"", "", // to_slot/session left empty for broadcasts
		"approval_request", p.PromptText, string(metadata), expiresAt,
	)
	if err != nil {
		log.Printf("[approval] store request error: %v", err)
		return
	}

	// Get the slot name for the broadcast
	slot, _ := s.db.FindSlotByName(s.db.MachineID(), "")
	slotName := ""
	// Look up slot name from the client's slot
	slots, _ := s.db.ListSlotsWithSessions(s.db.MachineID(), false)
	for _, sw := range slots {
		if sw.SlotID == client.SlotID {
			slotName = sw.SlotName
			break
		}
	}

	// Broadcast to all other clients
	broadcast := protocol.ApprovalBroadcast{
		RequestID:   msgID,
		SessionID:   p.SessionID,
		SlotID:      client.SlotID,
		SlotName:    slotName,
		PromptText:  p.PromptText,
		PromptHash:  p.PromptHash,
		Context:     p.Context,
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
		TTLSeconds:  p.TTLSeconds,
	}

	payload, _ := json.Marshal(broadcast)
	s.broadcast(&protocol.Envelope{
		Type:    protocol.TypeApprovalBroadcast,
		Payload: payload,
	}, client.SessionID)

	// Update the agent's state to blocked
	s.db.UpdateSessionHeartbeat(p.SessionID, "blocked_on_approval", p.PromptText, "")

	log.Printf("[approval] broadcast request from %s: %s", slotName, p.PromptText)

	_ = slot // suppress unused warning
}

func (s *Server) handleApprovalResponse(client *Client, env *protocol.Envelope) {
	var p protocol.ApprovalResponsePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[approval] invalid response payload: %v", err)
		return
	}

	// Resolve target slot by name
	slot, err := s.db.FindSlotByName(s.db.MachineID(), p.TargetSlotName)
	if err != nil || slot == nil {
		log.Printf("[approval] target slot '%s' not found", p.TargetSlotName)
		s.sendResponse(client, "approval_result", env.RequestID, map[string]string{
			"error":   "not_found",
			"message": "No agent named '@" + p.TargetSlotName + "'",
		})
		return
	}

	if !slot.CurrentSessionID.Valid {
		s.sendResponse(client, "approval_result", env.RequestID, map[string]string{
			"error":   "inactive",
			"message": "Agent '" + p.TargetSlotName + "' has no active session",
		})
		return
	}

	targetSessionID := slot.CurrentSessionID.String

	// Route approval/denial ONLY to the wrapper (not MCP subscribers).
	// The wrapper is the one that needs to inject keystrokes into the pty.
	now := time.Now().UTC().Format(time.RFC3339)

	if p.Action == "approve" {
		payload, _ := json.Marshal(protocol.ApprovalGranted{
			RequestID:  p.RequestID,
			PromptHash: p.PromptHash,
			ApprovedBy: client.SessionID,
			ApprovedAt: now,
		})
		s.sendToWrapperOnly(targetSessionID, &protocol.Envelope{
			Type:    protocol.TypeApprovalGranted,
			Payload: payload,
		})
		log.Printf("[approval] granted for %s by %s", p.TargetSlotName, client.SessionID)
	} else {
		payload, _ := json.Marshal(protocol.ApprovalDenied{
			RequestID:  p.RequestID,
			PromptHash: p.PromptHash,
			DeniedBy:   client.SessionID,
			DeniedAt:   now,
		})
		s.sendToWrapperOnly(targetSessionID, &protocol.Envelope{
			Type:    protocol.TypeApprovalDenied,
			Payload: payload,
		})
		log.Printf("[approval] denied for %s by %s", p.TargetSlotName, client.SessionID)
	}

	// Confirm to the sender
	s.sendResponse(client, "approval_result", env.RequestID, map[string]string{
		"status": "routed",
		"action": p.Action,
		"target": p.TargetSlotName,
	})
}
