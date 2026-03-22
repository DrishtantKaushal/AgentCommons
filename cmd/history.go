package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/config"
	"github.com/DrishtantKaushal/AgentCommons/internal/daemon"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

func HistoryCmd() *cobra.Command {
	var showAll bool
	var limit int

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show message history between terminals",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			addr := daemon.Addr(cfg.Port)

			queryLimit := limit
			if showAll {
				queryLimit = 10000
			}
			if queryLimit <= 0 {
				queryLimit = 20
			}

			msgs, err := queryHistory(addr, queryLimit)
			if err != nil {
				fmt.Println("Commons daemon: not running")
				return nil
			}

			if len(msgs) == 0 {
				fmt.Println("No messages yet")
				return nil
			}

			fmt.Printf("MESSAGE HISTORY (%d messages)\n\n", len(msgs))
			// Messages come in DESC order from DB; display oldest first
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				ts := formatTimestamp(m.CreatedAt)
				typeLabel := "msg"
				if m.Type == "task" {
					typeLabel = "task"
				}
				statusLabel := m.Status
				fmt.Printf("  %s  %-5s  #%-12s -> #%-12s  [%s]\n", ts, typeLabel, m.FromName, m.ToName, statusLabel)
				fmt.Printf("    %s\n\n", m.Content)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all messages (not just last 20)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Number of messages to show")

	return cmd
}

func queryHistory(addr string, limit int) ([]protocol.MessageHistoryItem, error) {
	u := url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqID := fmt.Sprintf("history-%d", time.Now().UnixMilli())
	msg := protocol.Envelope{
		Type:      protocol.TypeListMessages,
		RequestID: reqID,
		Payload:   mustMarshalJSON(protocol.ListMessagesPayload{Limit: limit}),
	}
	if err := conn.WriteJSON(msg); err != nil {
		return nil, err
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	var env protocol.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}

	var resp protocol.ListMessagesResponse
	if err := json.Unmarshal(env.Payload, &resp); err != nil {
		return nil, err
	}

	return resp.Messages, nil
}

func formatTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try SQLite default format
		t, err = time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			return ts
		}
	}
	return t.Local().Format("Jan 02 15:04")
}
