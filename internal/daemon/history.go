package daemon

import (
	"encoding/json"
	"log"

	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
)

func (s *Server) handleListMessages(client *Client, env *protocol.Envelope) {
	var p protocol.ListMessagesPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[history] invalid payload: %v", err)
		return
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	msgs, err := s.db.ListMessages(s.db.MachineID(), limit)
	if err != nil {
		log.Printf("[history] db error: %v", err)
		s.sendResponse(client, "list_messages_response", env.RequestID, map[string]string{
			"error":   "db_error",
			"message": err.Error(),
		})
		return
	}

	items := make([]protocol.MessageHistoryItem, 0, len(msgs))
	for _, m := range msgs {
		items = append(items, protocol.MessageHistoryItem{
			ID:        m.ID,
			FromName:  m.FromName,
			ToName:    m.ToName,
			Content:   m.Content,
			Type:      m.MsgType,
			Status:    m.Status,
			CreatedAt: m.CreatedAt,
		})
	}

	s.sendResponse(client, "list_messages_response", env.RequestID, protocol.ListMessagesResponse{
		Messages: items,
	})
}
