package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/config"
	"github.com/DrishtantKaushal/AgentCommons/internal/daemon"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

func ApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve @name",
		Short: "Approve a pending approval request for the named agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendApprovalAction(args[0], "approve")
		},
	}
}

func DenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny @name",
		Short: "Deny a pending approval request for the named agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendApprovalAction(args[0], "deny")
		},
	}
}

func sendApprovalAction(target, action string) error {
	name := strings.TrimPrefix(target, "@")
	cfg := config.Default()
	addr := daemon.Addr(cfg.Port)

	// First, get the agent's current state to find the prompt hash
	agents, err := queryAgents(addr)
	if err != nil {
		return fmt.Errorf("cannot connect to daemon: %w", err)
	}

	var targetAgent *protocol.SlotInfo
	for i, a := range agents {
		if strings.EqualFold(a.SlotName, name) {
			targetAgent = &agents[i]
			break
		}
	}

	if targetAgent == nil {
		available := make([]string, 0, len(agents))
		for _, a := range agents {
			available = append(available, a.SlotName)
		}
		return fmt.Errorf("no agent named '@%s'. Available: %s", name, strings.Join(available, ", "))
	}

	if targetAgent.ActiveSession == nil {
		return fmt.Errorf("agent '%s' has no active session", name)
	}

	if targetAgent.ActiveSession.State != "blocked_on_approval" {
		return fmt.Errorf("agent '%s' is not blocked on approval (state: %s)", name, targetAgent.ActiveSession.State)
	}

	// Connect and send approval response
	u := url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	reqID := fmt.Sprintf("approve-%d", time.Now().UnixMilli())
	msg := protocol.Envelope{
		Type:      protocol.TypeApprovalResponse,
		RequestID: reqID,
		Payload: mustMarshalJSON(protocol.ApprovalResponsePayload{
			TargetSlotName: name,
			Action:         action,
			PromptHash:     "", // Daemon will route to wrapper which validates
		}),
	}
	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("send %s: %w", action, err)
	}

	// Read confirmation
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var env protocol.Envelope
	json.Unmarshal(raw, &env)

	var result map[string]string
	json.Unmarshal(env.Payload, &result)

	if result["error"] != "" {
		return fmt.Errorf("%s: %s", result["error"], result["message"])
	}

	if action == "approve" {
		fmt.Printf("Approved: %s\n%s resumed.\n", targetAgent.ActiveSession.StateDetail, name)
	} else {
		fmt.Printf("Denied: %s\n%s received denial.\n", targetAgent.ActiveSession.StateDetail, name)
	}

	return nil
}
