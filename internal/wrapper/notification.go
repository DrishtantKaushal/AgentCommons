package wrapper

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/protocol"
	ws "github.com/gorilla/websocket"
)

// NotificationState tracks what the notification manager is currently doing.
type NotificationState int

const (
	// StateIdle means no notification is displayed and no interception is active.
	StateIdle NotificationState = iota
	// StateNotificationActive means a notification is rendered and keystrokes
	// 1-3 are intercepted (unless a CC prompt is active).
	StateNotificationActive
	// StateCCPromptActive means a Claude Code prompt was detected while a
	// notification was pending. Keystrokes pass through; the notification
	// renders in queued mode.
	StateCCPromptActive
)

// PendingNotification holds one inbound approval broadcast awaiting user action.
type PendingNotification struct {
	SlotName   string
	PromptText string
	PromptHash string
	RequestID  string
	ReceivedAt time.Time
	Rendered   bool
}

// NotificationManager handles the lifecycle of inline approval notifications:
// enqueue, render, keystroke interception, clearing, auto-dismiss, and
// sending the approval response back through the daemon.
type NotificationManager struct {
	mu   sync.Mutex
	state NotificationState

	current *PendingNotification
	queue   []*PendingNotification

	detector      *Detector      // CC prompt detector
	renderedLines int            // how many lines the notification occupies
	lastStdinActivity time.Time  // when stdin last received data
	recentStdinBytes  int        // bytes received in the current burst

	// wsConn and sessionID are used to send approval_response back to the daemon.
	wsConn    *connHolder
	sessionID string
	slotName  string // this terminal's own slot name (for "approved_by")

	// stdoutMu serializes writes to os.Stdout with the pty output loop.
	stdoutMu *sync.Mutex
}

// NewNotificationManager creates a NotificationManager wired to the given
// detector and WebSocket connection.
func NewNotificationManager(detector *Detector, wsConn *connHolder, sessionID, slotName string, stdoutMu *sync.Mutex) *NotificationManager {
	return &NotificationManager{
		state:     StateIdle,
		detector:  detector,
		wsConn:    wsConn,
		sessionID: sessionID,
		slotName:  slotName,
		stdoutMu:  stdoutMu,
	}
}

// Enqueue adds a notification to the queue and activates it if idle.
func (nm *NotificationManager) Enqueue(n *PendingNotification) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nm.current == nil {
		nm.current = n
		nm.state = StateNotificationActive
		nm.render()
	} else {
		nm.queue = append(nm.queue, n)
	}
}

// TrackInput records stdin activity for the typing-detection heuristic.
// Called from the input loop on every read.
func (nm *NotificationManager) TrackInput(n int) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	now := time.Now()
	// If the last activity was more than 200ms ago, reset the burst counter.
	if now.Sub(nm.lastStdinActivity) > 200*time.Millisecond {
		nm.recentStdinBytes = 0
	}
	nm.recentStdinBytes += n
	nm.lastStdinActivity = now
}

// HandleKeystroke checks whether a single-byte keystroke should be consumed
// by the notification system. Returns true if the key was consumed (do NOT
// forward to the pty), false if it should pass through.
func (nm *NotificationManager) HandleKeystroke(key byte) bool {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Only intercept when a notification is active.
	if nm.state != StateNotificationActive {
		return false
	}
	if nm.current == nil {
		return false
	}

	// Priority check: if a CC prompt is active, don't intercept -- let the
	// keystroke flow to Claude Code's own prompt handler.
	if nm.detector.Current() != nil {
		return false
	}

	// Typing heuristic: if the user typed >1 byte in the last 200ms they are
	// in the middle of composing text. Don't intercept.
	if time.Since(nm.lastStdinActivity) < 200*time.Millisecond && nm.recentStdinBytes > 1 {
		return false
	}

	switch key {
	case '1': // Approve
		nm.approve()
		return true
	case '2': // Deny
		nm.deny()
		return true
	case '3': // Dismiss
		nm.dismiss()
		return true
	default:
		return false
	}
}

// IsActive returns true when a notification is rendered and interception is on.
func (nm *NotificationManager) IsActive() bool {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.state == StateNotificationActive && nm.current != nil
}

// Tick is called once per second. It handles auto-dismiss after 60s and
// re-checks whether a CC prompt has appeared or cleared.
func (nm *NotificationManager) Tick() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nm.current == nil {
		return
	}

	// --- CC prompt coexistence ---
	ccActive := nm.detector.Current() != nil

	if ccActive && nm.state == StateNotificationActive {
		// A CC prompt just appeared. Switch to queued rendering.
		nm.clear()
		nm.state = StateCCPromptActive
		nm.renderQueued()
	} else if !ccActive && nm.state == StateCCPromptActive {
		// CC prompt was handled. Re-activate the notification.
		nm.clear()
		nm.state = StateNotificationActive
		nm.render()
	}

	// --- Auto-dismiss after 60 seconds ---
	if time.Since(nm.current.ReceivedAt) >= 60*time.Second {
		slotName := nm.current.SlotName
		nm.clear()
		nm.stdoutMu.Lock()
		fmt.Fprintf(os.Stderr, "\x1b[90m[commons] auto-dismissed: %s\x1b[0m\r\n", slotName)
		nm.stdoutMu.Unlock()
		nm.current = nil
		nm.state = StateIdle
		nm.activateNext()
	}
}

// ---------------------------------------------------------------------------
// Internal (must be called with nm.mu held)
// ---------------------------------------------------------------------------

// render sets the terminal title bar to show the notification.
// Uses OSC 0 escape sequence to avoid corrupting Claude Code's Ink TUI —
// the title bar is zero-cursor-impact, unlike inline stdout writes.
func (nm *NotificationManager) render() {
	if nm.current == nil {
		return
	}

	// Set terminal title to show the notification
	title := fmt.Sprintf("[commons] %s needs approval: %s (1=Approve 2=Deny 3=Dismiss)",
		nm.current.SlotName, nm.current.PromptText)

	nm.stdoutMu.Lock()
	// OSC 0 sets both window title and icon name. \x07 is BEL (string terminator).
	fmt.Fprintf(os.Stdout, "\x1b]0;%s\x07", title)
	// Fire BEL to get user's attention (flashes/bounces the terminal)
	os.Stdout.Write([]byte("\x07"))
	// Write inline notification text to stderr to avoid corrupting Ink's TUI.
	text := fmt.Sprintf(
		"\r\n\x1b[33m[commons]\x1b[0m \x1b[1m%s\x1b[0m needs approval: %s\r\n"+
			"  \x1b[32m1 Approve\x1b[0m   \x1b[31m2 Deny\x1b[0m   \x1b[90m3 Dismiss\x1b[0m\r\n",
		nm.current.SlotName, nm.current.PromptText,
	)
	os.Stderr.Write([]byte(text))
	nm.stdoutMu.Unlock()

	nm.renderedLines = 0 // no lines rendered in viewport
	nm.current.Rendered = true
}

// renderQueued sets the terminal title bar to show a queued notification
// (CC prompt is active, so keystrokes pass through to Claude Code).
func (nm *NotificationManager) renderQueued() {
	if nm.current == nil {
		return
	}

	title := fmt.Sprintf("[commons] (queued) %s needs approval: %s",
		nm.current.SlotName, nm.current.PromptText)

	nm.stdoutMu.Lock()
	fmt.Fprintf(os.Stdout, "\x1b]0;%s\x07", title)
	// Write queued notification text to stderr to avoid corrupting Ink's TUI.
	text := fmt.Sprintf(
		"\r\n\x1b[33m[commons]\x1b[0m \x1b[90m(queued)\x1b[0m \x1b[1m%s\x1b[0m needs approval: %s\r\n",
		nm.current.SlotName, nm.current.PromptText,
	)
	os.Stderr.Write([]byte(text))
	nm.stdoutMu.Unlock()

	nm.renderedLines = 0
	nm.current.Rendered = true
}

// clear resets the terminal title bar to empty (the terminal emulator
// will fall back to its default title).
func (nm *NotificationManager) clear() {
	nm.stdoutMu.Lock()
	fmt.Fprintf(os.Stdout, "\x1b]0;\x07")
	nm.stdoutMu.Unlock()
	nm.renderedLines = 0
}

// approve resets the title bar, sends an approval response to the daemon,
// and writes a single-line confirmation inline.
func (nm *NotificationManager) approve() {
	slotName := nm.current.SlotName
	nm.clear()
	nm.sendApproval("approve")
	nm.stdoutMu.Lock()
	fmt.Fprintf(os.Stderr, "\x1b[32m[commons] Approved: %s\x1b[0m\r\n", slotName)
	nm.stdoutMu.Unlock()
	nm.current = nil
	nm.state = StateIdle
	nm.activateNext()
}

// deny resets the title bar, sends a denial response to the daemon,
// and writes a single-line confirmation inline.
func (nm *NotificationManager) deny() {
	slotName := nm.current.SlotName
	nm.clear()
	nm.sendApproval("deny")
	nm.stdoutMu.Lock()
	fmt.Fprintf(os.Stderr, "\x1b[31m[commons] Denied: %s\x1b[0m\r\n", slotName)
	nm.stdoutMu.Unlock()
	nm.current = nil
	nm.state = StateIdle
	nm.activateNext()
}

// dismiss resets the title bar without sending any response to the daemon.
// The blocked agent stays blocked; the user can handle it later via /inbox.
func (nm *NotificationManager) dismiss() {
	slotName := nm.current.SlotName
	nm.clear()
	nm.stdoutMu.Lock()
	fmt.Fprintf(os.Stderr, "\x1b[90m[commons] Dismissed: %s (still in /inbox)\x1b[0m\r\n", slotName)
	nm.stdoutMu.Unlock()
	nm.current = nil
	nm.state = StateIdle
	nm.activateNext()
}

// sendApproval delivers an approval_response envelope to the daemon.
// Uses connHolder.writeMessage to serialize with heartbeat and other writers.
func (nm *NotificationManager) sendApproval(action string) {
	if nm.wsConn.get() == nil {
		return
	}
	payload, _ := json.Marshal(protocol.ApprovalResponsePayload{
		TargetSlotName: nm.current.SlotName,
		RequestID:      nm.current.RequestID,
		Action:         action,
		PromptHash:     nm.current.PromptHash,
	})
	data, _ := json.Marshal(protocol.Envelope{
		Type:      protocol.TypeApprovalResponse,
		RequestID: fmt.Sprintf("notif-resp-%d", time.Now().UnixMilli()),
		Payload:   payload,
	})
	nm.wsConn.writeMessage(ws.TextMessage, data)
}

// activateNext promotes the next queued notification to current (if any).
func (nm *NotificationManager) activateNext() {
	if len(nm.queue) == 0 {
		return
	}
	nm.current = nm.queue[0]
	nm.queue = nm.queue[1:]
	nm.state = StateNotificationActive
	nm.render()
}

