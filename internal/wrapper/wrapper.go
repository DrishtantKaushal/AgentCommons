package wrapper

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/config"
	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/DrishtantKaushal/AgentCommons/internal/naming"
	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
	ptyPkg "github.com/creack/pty/v2"
	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

// connHolder wraps a WebSocket connection with a mutex to prevent data races
// when multiple goroutines (heartbeat, output, listener, defer) access it.
// IMPORTANT: All writes to the WebSocket MUST go through writeMessage() to
// serialize concurrent writers (heartbeat, notification response, deregister).
// gorilla/websocket does NOT support concurrent WriteMessage calls.
type connHolder struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (h *connHolder) get() *websocket.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn
}

func (h *connHolder) clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conn = nil
}

// writeMessage serializes all WebSocket writes through the holder's mutex.
// This prevents concurrent WriteMessage calls from corrupting frames.
func (h *connHolder) writeMessage(messageType int, data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return fmt.Errorf("connection closed")
	}
	return h.conn.WriteMessage(messageType, data)
}

// stdoutMu serializes all writes to os.Stdout so that pty output and
// notification rendering never interleave.
var stdoutMu sync.Mutex

// Run executes the session wrapper for `commons run <tool>`.
func Run(tool string, nameOverride string) error {
	// Log to a file for debugging the approval flow.
	// Production: change back to io.Discard
	logFile, logErr := os.OpenFile(filepath.Join(os.TempDir(), "commons-wrapper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logErr != nil {
		log.SetOutput(io.Discard)
	} else {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	cfg := config.Default()

	// Resolve terminal name
	terminalName := nameOverride
	if terminalName == "" {
		terminalName = naming.Resolve()
	}

	// Start caffeinate
	caffCmd := startCaffeinate()
	defer stopCaffeinate(caffCmd)

	// Connect to daemon
	rawConn, sessionID, slotID, assignedName, bootstrapMsg, err := connectAndRegister(cfg, terminalName, tool)
	if err != nil {
		log.Printf("[wrapper] daemon connection failed, running in passthrough mode: %v", err)
		// Passthrough mode — just exec the tool directly
		return execPassthrough(tool)
	}

	// Wrap the connection in a mutex-protected holder to prevent data races
	// between goroutines that read/write wsConn (bug-009).
	wsConn := &connHolder{conn: rawConn}

	// Persist the assigned slot name so that reopening this terminal
	// reconnects to the same slot instead of generating a new name.
	naming.SaveSlotName(assignedName)
	// Record the active wrapper session for this terminal so the MCP server
	// can detect wrapper mode.
	naming.SaveWrapperSession(sessionID)
	// Update terminalName to the actually assigned name (may differ from
	// the initial candidate if there was a collision and retry).
	terminalName = assignedName
	defer func() {
		naming.ClearWrapperSession()
		if c := wsConn.get(); c != nil {
			// Deregister — use sendJSONSafe to serialize with other writers
			sendJSONSafe(wsConn, protocol.Envelope{
				Type:      protocol.TypeDeregister,
				RequestID: "dereg-1",
				Payload:   mustMarshal(protocol.DeregisterPayload{SessionID: sessionID}),
			})
			c.Close()
			wsConn.clear()
		}
	}()

	// Print registration banner
	if bootstrapMsg != "" {
		fmt.Println(bootstrapMsg)
	}

	// Resolve the tool binary
	toolPath, err := exec.LookPath(tool)
	if err != nil {
		return fmt.Errorf("%s not found on $PATH", tool)
	}

	// Build command args — inject commons flags for claude
	var toolArgs []string
	if tool == "claude" {
		// Add plugin dir for commons skills (commons:status, commons:inbox, etc.)
		pluginDir := findPluginDir()
		if pluginDir != "" {
			toolArgs = append(toolArgs, "--plugin-dir", pluginDir)
		}
		// Enable channel protocol for live message push
		toolArgs = append(toolArgs, "--dangerously-load-development-channels", "server:commons")
	}

	// Allocate pty and start the tool
	toolCmd := exec.Command(toolPath, toolArgs...)
	toolCmd.Env = append(os.Environ(),
		"COMMONS_SLOT_ID="+slotID,
		"COMMONS_SESSION_ID="+sessionID,
		"COMMONS_SLOT_NAME="+terminalName,
	)

	ptmx, err := ptyPkg.Start(toolCmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	// Set real terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Match pty size to terminal
	inheritSize(ptmx)

	// Create detector, injector, and notification manager
	detector := NewDetector()
	injector := NewInjector(ptmx, detector)
	notifMgr := NewNotificationManager(detector, wsConn, sessionID, terminalName, &stdoutMu)

	// Done channel
	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	// --- Goroutines ---

	// Output loop: master fd -> stdout + detector
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				stdoutMu.Lock()
				os.Stdout.Write(buf[:n])
				stdoutMu.Unlock()

				// Feed detector
				prompt := detector.Feed(buf[:n])
				if prompt != nil && wsConn.get() != nil {
					// Publish approval request — use sendJSONSafe to serialize writes
					sendJSONSafe(wsConn, protocol.Envelope{
						Type:      protocol.TypeApprovalRequest,
						RequestID: fmt.Sprintf("approval-%d", time.Now().UnixMilli()),
						Payload: mustMarshal(protocol.ApprovalRequestPayload{
							SessionID:  sessionID,
							SlotID:     slotID,
							PromptText: prompt.Text,
							PromptHash: prompt.Hash,
							Context:    prompt.Context,
							TTLSeconds: 600,
						}),
					})
				}
			}
			if err != nil {
				closeDone()
				return
			}
		}
	}()

	// Input loop: stdin -> master fd (with notification keystroke interception)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				// Track typing activity for the 200ms heuristic
				notifMgr.TrackInput(n)

				// Try to intercept single-byte digits when notification is active
				if n == 1 && notifMgr.HandleKeystroke(buf[0]) {
					continue // consumed by notification, don't forward to pty
				}

				ptmx.Write(buf[:n])
			}
			if err != nil {
				closeDone()
				return
			}
		}
	}()

	// Heartbeat loop
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if wsConn.get() == nil {
					continue
				}
				state := "idle"
				stateDetail := ""
				if p := detector.Current(); p != nil {
					state = "blocked_on_approval"
					stateDetail = p.Text
				}
				sendJSONSafe(wsConn, protocol.Envelope{
					Type:      protocol.TypeHeartbeat,
					RequestID: fmt.Sprintf("hb-%d", time.Now().UnixMilli()),
					Payload: mustMarshal(protocol.HeartbeatPayload{
						SessionID:   sessionID,
						State:       state,
						StateDetail: stateDetail,
						CWD:         getCWD(),
					}),
				})
			case <-done:
				return
			}
		}
	}()

	// Notification auto-dismiss ticker: calls Tick() every second to handle
	// 60s timeout and CC prompt coexistence transitions.
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				notifMgr.Tick()
			case <-done:
				return
			}
		}
	}()

	// WebSocket listener for approval events and daemon broadcasts.
	// The goroutine must hold a direct reference to the underlying *websocket.Conn
	// for ReadMessage (which blocks). We only use wsConn.clear() for shutdown, not
	// for reads, because ReadMessage is not protected by the holder mutex — reads
	// and writes on gorilla/websocket are safe to do concurrently on different
	// goroutines.
	go func() {
		c := wsConn.get()
		if c == nil {
			return
		}
		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				// Connection closed or errored — stop listening.
				// Do NOT nil out wsConn here; the defer block handles cleanup.
				return
			}
			var env protocol.Envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				continue
			}
			switch env.Type {
			case protocol.TypeApprovalGranted:
				var p protocol.ApprovalGranted
				json.Unmarshal(env.Payload, &p)
				if !injector.Approve(p.PromptHash) {
					log.Printf("[wrapper] approval could not be applied — prompt has changed")
				}
			case protocol.TypeApprovalDenied:
				var p protocol.ApprovalDenied
				json.Unmarshal(env.Payload, &p)
				if !injector.Deny(p.PromptHash) {
					log.Printf("[wrapper] denial could not be applied — prompt has changed")
				}
			case protocol.TypeApprovalBroadcast:
				var p protocol.ApprovalBroadcast
				json.Unmarshal(env.Payload, &p)
				notifMgr.Enqueue(&PendingNotification{
					SlotName:   p.SlotName,
					PromptText: p.PromptText,
					PromptHash: p.PromptHash,
					RequestID:  p.RequestID,
					ReceivedAt: time.Now(),
				})
			case protocol.TypeAgentStateChanged:
				// Another agent changed state (e.g. disconnected). This is
				// informational — ignore it. Previously unhandled, this case
				// fell through silently which was fine, but we handle it
				// explicitly to avoid confusion during debugging.
				log.Printf("[wrapper] agent state changed event received (informational)")
			case protocol.TypeServerShuttingDown:
				log.Printf("[wrapper] daemon shutting down, entering passthrough mode")
				wsConn.clear()
				return // Stop listening — connection is no longer valid
			}
		}
	}()

	// Signal handler: SIGWINCH for terminal resize
	sigWinch := make(chan os.Signal, 1)
	signal.Notify(sigWinch, syscall.SIGWINCH)
	go func() {
		for range sigWinch {
			inheritSize(ptmx)
		}
	}()

	// Wait for child to exit
	<-done
	exitCode := 0
	if err := toolCmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Restore terminal and exit
	term.Restore(int(os.Stdin.Fd()), oldState)
	os.Exit(exitCode)
	return nil
}

func connectAndRegister(cfg config.Config, terminalName, tool string) (*websocket.Conn, string, string, string, string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)

	// Check if daemon is running
	healthURL := fmt.Sprintf("http://%s/health", addr)
	if _, err := http.Get(healthURL); err != nil {
		// Auto-launch daemon
		if err := autoLaunchDaemon(addr); err != nil {
			return nil, "", "", "", "", fmt.Errorf("auto-launch daemon: %w", err)
		}
	}

	// Connect via WebSocket
	u := url.URL{Scheme: "ws", Host: addr, Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, "", "", "", "", fmt.Errorf("websocket dial: %w", err)
	}

	// Try registration, retry with wordlist name on collision
	nameToUse := terminalName
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, regErr := tryRegister(conn, nameToUse)
		if regErr != nil {
			conn.Close()
			return nil, "", "", "", "", regErr
		}
		if result.resp.Error == "slot_claimed" {
			// Name collision — try a wordlist name instead
			nameToUse = naming.WordlistName()
			log.Printf("[wrapper] slot '%s' claimed, retrying as '%s'", terminalName, nameToUse)
			continue
		}
		if result.resp.Error != "" {
			conn.Close()
			return nil, "", "", "", "", fmt.Errorf("%s", result.resp.Message)
		}
		// Success — return the actually assigned name along with connection info
		return conn, result.resp.SessionID, result.resp.SlotID, nameToUse, buildBanner(nameToUse, &result.resp), nil
	}

	conn.Close()
	return nil, "", "", "", "", fmt.Errorf("failed to register after %d attempts", maxRetries)
}

type registerResult struct {
	resp protocol.RegisterResponse
}

func tryRegister(conn *websocket.Conn, name string) (*registerResult, error) {
	cwd := getCWD()
	reqID := fmt.Sprintf("register-%d", time.Now().UnixMilli())
	sendJSON(conn, protocol.Envelope{
		Type:      protocol.TypeRegister,
		RequestID: reqID,
		Payload: mustMarshal(protocol.RegisterPayload{
			AgentType:       "claude-code",
			TerminalName:    name,
			PID:             os.Getpid(),
			CWD:             cwd,
			ClaudeSessionID: os.Getenv("CLAUDE_SESSION_ID"),
		}),
	})

	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read register response: %w", err)
	}

	var env protocol.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse register response: %w", err)
	}

	var resp protocol.RegisterResponse
	if err := json.Unmarshal(env.Payload, &resp); err != nil {
		return nil, fmt.Errorf("parse register payload: %w", err)
	}

	return &registerResult{resp: resp}, nil
}

func buildBanner(terminalName string, resp *protocol.RegisterResponse) string {
	return terminalName
}

func autoLaunchDaemon(addr string) error {
	exe, err := os.Executable()
	if err != nil {
		// Fall back to looking up "commons" on PATH
		exe = "commons"
	}

	child := exec.Command(exe, "server", "start", "--foreground")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	child.Stdout = nil
	child.Stderr = nil
	child.Stdin = nil

	if err := child.Start(); err != nil {
		return err
	}
	child.Process.Release()

	// Poll health
	healthURL := fmt.Sprintf("http://%s/health", addr)
	for i := 0; i < 12; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			return nil
		}
	}
	return fmt.Errorf("daemon health check failed after 6s")
}

func execPassthrough(tool string) error {
	toolPath, err := exec.LookPath(tool)
	if err != nil {
		return fmt.Errorf("%s not found on $PATH", tool)
	}
	return syscall.Exec(toolPath, []string{tool}, os.Environ())
}

func inheritSize(ptmx *os.File) {
	width, height, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	ptyPkg.Setsize(ptmx, &ptyPkg.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
}

func findPluginDir() string {
	// Check known locations for the commons plugin
	candidates := []string{
		filepath.Join(db.CommonsDir(), "plugin"),
	}

	// Also check relative to the executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "plugin"),
			filepath.Join(exeDir, "..", "plugin"),
			filepath.Join(exeDir, "..", "src", "plugin"),
		)
	}

	for _, c := range candidates {
		pluginJSON := filepath.Join(c, ".claude-plugin", "plugin.json")
		if _, err := os.Stat(pluginJSON); err == nil {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
	}
	return ""
}

func getCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func sendJSON(conn *websocket.Conn, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[wrapper] marshal error: %v", err)
		return
	}
	conn.WriteMessage(websocket.TextMessage, data)
}

// sendJSONSafe serializes a JSON message through the connHolder's write mutex.
// Use this instead of sendJSON for all writes after goroutines have started.
func sendJSONSafe(h *connHolder, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[wrapper] marshal error: %v", err)
		return
	}
	h.writeMessage(websocket.TextMessage, data)
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
