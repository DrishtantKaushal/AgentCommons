package daemon

import (
	"log"

	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
)

// buildBootstrap generates a bootstrap payload for a reclaimed slot.
func (s *Server) buildBootstrap(slot *db.Slot) *protocol.SlotContext {
	ctx := &protocol.SlotContext{
		PreviousSessions: slot.TotalSessions,
	}

	// Last session info
	if slot.LastSessionEndedAt.Valid {
		ctx.LastSession = &protocol.LastSessionInfo{
			EndedAt: slot.LastSessionEndedAt.String,
		}
		if slot.LastCWD.Valid {
			ctx.LastSession.LastCWD = slot.LastCWD.String
		}
		if slot.LastBranch.Valid {
			ctx.LastSession.LastBranch = slot.LastBranch.String
		}
		if slot.LastStateDetail.Valid {
			ctx.LastSession.LastStateDetail = slot.LastStateDetail.String
		}
	}

	// Pending messages
	msgs, err := s.db.GetPendingMessages(slot.SlotID)
	if err != nil {
		log.Printf("[bootstrap] get pending messages error: %v", err)
	} else {
		for _, m := range msgs {
			ctx.PendingMessages = append(ctx.PendingMessages, protocol.PendingMessage{
				ID:       m.ID,
				FromSlot: m.FromSlotName,
				Content:  m.Content,
				SentAt:   m.CreatedAt,
			})
		}

		// Mark messages as delivered
		var ids []string
		for _, m := range msgs {
			ids = append(ids, m.ID)
		}
		if len(ids) > 0 {
			if err := s.db.MarkMessagesDelivered(ids); err != nil {
				log.Printf("[bootstrap] mark delivered error: %v", err)
			}
		}
	}

	return ctx
}
