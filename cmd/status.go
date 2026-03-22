package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/config"
	"github.com/DrishtantKaushal/AgentCommons/internal/daemon"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

func StatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show all registered agents and their state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			addr := daemon.Addr(cfg.Port)

			agents, err := queryAgents(addr)
			if err != nil {
				fmt.Println("Commons daemon: not running")
				return nil
			}

			if len(agents) == 0 {
				fmt.Println("No agents registered")
				return nil
			}

			alive := 0
			inactive := 0
			for _, a := range agents {
				if a.ActiveSession != nil {
					alive++
				} else {
					inactive++
				}
			}

			header := fmt.Sprintf("AGENTS (%d alive", alive)
			if inactive > 0 {
				header += fmt.Sprintf(", %d inactive", inactive)
			}
			header += ")"
			fmt.Println(header)

			for _, a := range agents {
				if a.ActiveSession != nil {
					s := a.ActiveSession
					state := s.State
					line := fmt.Sprintf("  %-20s %s  %-22s %s", a.SlotName, s.AgentType, state, shortenPath(s.CWD))
					fmt.Println(line)

					if state == "blocked_on_approval" && s.StateDetail != "" {
						fmt.Printf("    Awaiting: \"%s\"\n", s.StateDetail)
						fmt.Printf("    /approve @%s\n", a.SlotName)
					}
				} else {
					lastInfo := "inactive"
					if a.LastCWD != "" {
						lastInfo = fmt.Sprintf("inactive   last cwd: %s", shortenPath(a.LastCWD))
					}
					fmt.Printf("  %-20s %s\n", a.SlotName, lastInfo)
				}
			}

			return nil
		},
	}
}

func queryAgents(addr string) ([]protocol.SlotInfo, error) {
	u := url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqID := fmt.Sprintf("status-%d", time.Now().UnixMilli())
	msg := protocol.Envelope{
		Type:      protocol.TypeListAgents,
		RequestID: reqID,
		Payload:   mustMarshalJSON(protocol.ListAgentsPayload{Filter: "all"}),
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

	var resp protocol.ListAgentsResponse
	if err := json.Unmarshal(env.Payload, &resp); err != nil {
		return nil, err
	}

	return resp.Agents, nil
}

func shortenPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func mustMarshalJSON(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
