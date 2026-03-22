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

func PushCmd() *cobra.Command {
	var isTask bool

	cmd := &cobra.Command{
		Use:   "push @name message...",
		Short: "Send a message or task to another agent terminal",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimPrefix(args[0], "@")
			name = strings.TrimPrefix(name, "#")
			message := strings.Join(args[1:], " ")

			msgType := "message"
			if isTask {
				msgType = "task"
			}

			cfg := config.Default()
			addr := daemon.Addr(cfg.Port)

			// Connect to daemon
			u := url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
			conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				return fmt.Errorf("cannot connect to daemon: %w", err)
			}
			defer conn.Close()

			// Send push message
			reqID := fmt.Sprintf("push-%d", time.Now().UnixMilli())
			msg := protocol.Envelope{
				Type:      "push_message",
				RequestID: reqID,
				Payload: mustMarshalJSON(PushPayload{
					TargetSlotName: name,
					Content:        message,
					MessageType:    msgType,
				}),
			}
			if err := conn.WriteJSON(msg); err != nil {
				return fmt.Errorf("send push: %w", err)
			}

			// Read response
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
				return fmt.Errorf("%s", result["message"])
			}

			label := "Message"
			if isTask {
				label = "Task"
			}
			fmt.Printf("%s sent to #%s\n", label, name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&isTask, "task", false, "Send as a task (actionable instruction) instead of a message")

	return cmd
}

type PushPayload struct {
	TargetSlotName string `json:"target_slot_name"`
	Content        string `json:"content"`
	MessageType    string `json:"message_type"`
}
