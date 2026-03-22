package daemon

import (
	"encoding/json"
	"log"

	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
)

func (s *Server) handleRegister(client *Client, env *protocol.Envelope) {
	var p protocol.RegisterPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[registry] invalid register payload: %v", err)
		return
	}

	machineID := s.db.MachineID()

	// Look up existing slot
	slot, err := s.db.FindSlotByName(machineID, p.TerminalName)
	if err != nil {
		log.Printf("[registry] slot lookup error: %v", err)
		s.sendResponse(client, protocol.TypeRegisterResponse, env.RequestID, protocol.RegisterResponse{
			Error:   "internal_error",
			Message: "Failed to look up slot",
		})
		return
	}

	if slot != nil && slot.CurrentSessionID.Valid {
		// Slot exists with an active session — check if the old session's PID is actually alive
		oldSession, _ := s.db.GetSession(slot.CurrentSessionID.String)
		if oldSession != nil && !isPIDAlive(oldSession.PID) {
			// Old session's process is dead — forcibly reclaim the slot
			log.Printf("[registry] slot '%s' has dead session (pid %d), reclaiming", p.TerminalName, oldSession.PID)
			s.db.MarkSessionDead(oldSession.SessionID, "crash")
			s.db.ClearSlotSession(slot.SlotID, oldSession.CWD, "", "inactive", "")
			s.db.AppendPresenceLog(oldSession.SessionID, slot.SlotID, oldSession.State, "dead", "stale_reclaim", "")
			s.removeClient(&Client{SessionID: oldSession.SessionID})
		} else {
			// Old session is genuinely alive — reject
			s.sendResponse(client, protocol.TypeRegisterResponse, env.RequestID, protocol.RegisterResponse{
				Error:   "slot_claimed",
				Message: "Slot '" + p.TerminalName + "' already has an active session. Choose a different name or close the existing session.",
			})
			return
		}
	}

	var isNewSlot bool
	var slotID string

	if slot == nil {
		// Create new slot
		newSlot, err := s.db.InsertSlot(p.TerminalName, machineID, s.db.UserID())
		if err != nil {
			log.Printf("[registry] insert slot error: %v", err)
			s.sendResponse(client, protocol.TypeRegisterResponse, env.RequestID, protocol.RegisterResponse{
				Error:   "internal_error",
				Message: "Failed to create slot",
			})
			return
		}
		slotID = newSlot.SlotID
		isNewSlot = true
	} else {
		// Reclaim existing slot
		slotID = slot.SlotID
		isNewSlot = false
	}

	// Create session
	session, err := s.db.InsertSession(slotID, p.AgentType, p.PID, p.CWD, p.RepoRoot, p.ClaudeSessionID)
	if err != nil {
		log.Printf("[registry] insert session error: %v", err)
		s.sendResponse(client, protocol.TypeRegisterResponse, env.RequestID, protocol.RegisterResponse{
			Error:   "internal_error",
			Message: "Failed to create session",
		})
		return
	}

	// Bind session to slot
	if err := s.db.UpdateSlotSession(slotID, session.SessionID); err != nil {
		log.Printf("[registry] update slot session error: %v", err)
	}

	// Increment session count
	if err := s.db.IncrementSlotSessions(slotID); err != nil {
		log.Printf("[registry] increment sessions error: %v", err)
	}

	// Register WebSocket client
	s.addClient(session.SessionID, slotID, client)
	client.ClientType = clientTypeWrapper

	// Build response
	resp := protocol.RegisterResponse{
		SessionID:     session.SessionID,
		SlotID:        slotID,
		IsNewSlot:     isNewSlot,
		ServerVersion: s.version,
	}

	// If reclaiming, build bootstrap payload
	if !isNewSlot {
		resp.SlotContext = s.buildBootstrap(slot)
	}

	s.sendResponse(client, protocol.TypeRegisterResponse, env.RequestID, resp)

	log.Printf("[registry] registered %s (slot=%s, session=%s, new=%v)", p.TerminalName, slotID, session.SessionID, isNewSlot)

	// Broadcast state change to other clients
	s.broadcastStateChange(session.SessionID, p.TerminalName, "", "idle", "")
}

func (s *Server) handleDeregister(client *Client, env *protocol.Envelope) {
	var p protocol.DeregisterPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		log.Printf("[registry] invalid deregister payload: %v", err)
		return
	}

	session, err := s.db.GetSession(p.SessionID)
	if err != nil || session == nil {
		log.Printf("[registry] deregister: session not found %s", p.SessionID)
		return
	}

	// Guard against double-deregister: if the session is already dead (e.g. the
	// wrapper already deregistered and now the MCP subscriber is sending a second
	// deregister for the same session), skip the DB mutations and broadcast.
	// This prevents spurious agent_state_changed events that confuse other
	// terminals into thinking they lost their daemon connection (bug-009).
	if session.IsAlive == 0 {
		log.Printf("[registry] deregister: session %s already dead, skipping", p.SessionID)
		s.removeClient(client)
		return
	}

	// Mark session dead
	if err := s.db.MarkSessionDead(p.SessionID, "clean_exit"); err != nil {
		log.Printf("[registry] mark session dead error: %v", err)
	}

	// Snapshot slot state and clear session binding
	if err := s.db.ClearSlotSession(session.SlotID, session.CWD, "", "inactive", ""); err != nil {
		log.Printf("[registry] clear slot session error: %v", err)
	}

	// Log presence transition
	s.db.AppendPresenceLog(p.SessionID, session.SlotID, session.State, "dead", "clean_exit", "")

	// Remove client
	s.removeClient(client)

	// Look up slot name for the broadcast so other terminals see which slot
	// disconnected (previously this was empty, causing "# disconnected").
	slotName := ""
	if slot, err := s.db.GetSlotByID(session.SlotID); err == nil && slot != nil {
		slotName = slot.SlotName
	}

	log.Printf("[registry] deregistered session %s (slot=%s, name=%s)", p.SessionID, session.SlotID, slotName)

	// Broadcast disconnection
	s.broadcastStateChange(p.SessionID, slotName, session.State, "disconnected", "clean_exit")
}

func (s *Server) handleListAgents(client *Client, env *protocol.Envelope) {
	var p protocol.ListAgentsPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		// Empty payload is fine — default to "all"
		p.Filter = "all"
	}

	aliveOnly := p.Filter == "alive"
	slots, err := s.db.ListSlotsWithSessions(s.db.MachineID(), aliveOnly)
	if err != nil {
		log.Printf("[registry] list agents error: %v", err)
		return
	}

	var agents []protocol.SlotInfo
	for _, sw := range slots {
		si := protocol.SlotInfo{
			SlotID:        sw.SlotID,
			SlotName:      sw.SlotName,
			CreatedAt:     sw.CreatedAt,
			LastState:     sw.LastState,
			TotalSessions: sw.TotalSessions,
		}
		if sw.LastStateDetail.Valid {
			si.LastStateDetail = sw.LastStateDetail.String
		}
		if sw.LastCWD.Valid {
			si.LastCWD = sw.LastCWD.String
		}
		if sw.Session != nil {
			si.ActiveSession = &protocol.SessionInfo{
				SessionID:     sw.Session.SessionID,
				AgentType:     sw.Session.AgentType,
				PID:           sw.Session.PID,
				CWD:           sw.Session.CWD,
				State:         sw.Session.State,
				StartedAt:     sw.Session.StartedAt,
				LastHeartbeat: sw.Session.LastHeartbeat,
			}
			if sw.Session.StateDetail.Valid {
				si.ActiveSession.StateDetail = sw.Session.StateDetail.String
			}
			si.LastState = sw.Session.State
		}
		agents = append(agents, si)
	}

	s.sendResponse(client, "list_agents_response", env.RequestID, protocol.ListAgentsResponse{
		Agents: agents,
	})
}

func (s *Server) broadcastStateChange(sessionID, slotName, oldState, newState, detail string) {
	payload, _ := json.Marshal(protocol.AgentStateChanged{
		SessionID: sessionID,
		SlotName:  slotName,
		OldState:  oldState,
		NewState:  newState,
		Detail:    detail,
	})
	s.broadcast(&protocol.Envelope{
		Type:    protocol.TypeAgentStateChanged,
		Payload: payload,
	}, sessionID)
}
