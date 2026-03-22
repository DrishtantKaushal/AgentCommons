package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
)

func (s *Server) handlePushMessage(client *Client, env *protocol.Envelope) {
	var p protocol.PushMessagePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[push] invalid payload: %v", err)
		return
	}

	// Resolve target slot
	slot, err := s.db.FindSlotByName(s.db.MachineID(), p.TargetSlotName)
	if err != nil || slot == nil {
		s.sendResponse(client, "push_response", env.RequestID, map[string]string{
			"error":   "not_found",
			"message": "No agent named '#" + p.TargetSlotName + "'",
		})
		return
	}

	// Resolve sender name
	senderName := ""
	if client.SlotID != "" {
		slots, _ := s.db.ListSlotsWithSessions(s.db.MachineID(), false)
		for _, sw := range slots {
			if sw.SlotID == client.SlotID {
				senderName = sw.SlotName
				break
			}
		}
	}
	if senderName == "" {
		senderName = "cli"
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Default message type to "message" if not specified
	msgType := p.MessageType
	if msgType == "" {
		msgType = "message"
	}

	// Store the message in SQLite (persists even if target is offline)
	// Include message_type in metadata JSON
	metadata := fmt.Sprintf(`{"message_type":%q}`, msgType)
	s.db.InsertMessage(
		client.SlotID, client.SessionID,
		slot.SlotID, "",
		"direct", p.Content, metadata, nil,
	)

	// Deliver message to the target session (registered client + subscribers).
	// This targeted delivery ensures only the intended terminal receives the message,
	// not all connected WebSocket clients.
	payload, _ := json.Marshal(protocol.MessagePush{
		FromSlotName: senderName,
		Content:      p.Content,
		MessageType:  msgType,
		SentAt:       now,
	})
	pushEnv := &protocol.Envelope{
		Type:    protocol.TypeMessagePush,
		Payload: payload,
	}

	if slot.CurrentSessionID.Valid {
		targetSessionID := slot.CurrentSessionID.String
		s.sendToSession(targetSessionID, pushEnv)
		log.Printf("[push] delivered message from %s to %s (session %s)", senderName, p.TargetSlotName, targetSessionID)
	} else {
		log.Printf("[push] message queued for offline slot %s", p.TargetSlotName)
	}

	s.sendResponse(client, "push_response", env.RequestID, map[string]string{
		"status": "sent",
		"target": p.TargetSlotName,
	})
}
