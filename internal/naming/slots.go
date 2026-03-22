package naming

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// slotMapping represents a persisted terminal-to-slot-name mapping.
type slotMapping struct {
	SlotName  string `json:"slot_name"`
	UpdatedAt string `json:"updated_at"`
}

// terminalSlotsFile returns the path to ~/.commons/terminal-slots.json.
func terminalSlotsFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".commons", "terminal-slots.json")
}

// TerminalKey returns a stable identifier for the current terminal.
// It uses the TTY device path (e.g., /dev/ttys003) which remains the same
// for the lifetime of a terminal tab/window. If the TTY can't be determined,
// it falls back to a hash of TERM_SESSION_ID (macOS Terminal/iTerm) or
// WINDOWID (X11 terminals).
func TerminalKey() string {
	// Strategy 1: Get the TTY device path via ttyname on stdin.
	// This is the most reliable — it persists across shell sessions in the
	// same terminal tab, and each terminal tab gets a unique TTY.
	if ttyName, err := ttyname(); ttyName != "" && err == nil {
		return ttyName
	}

	// Strategy 2: macOS Terminal.app and iTerm2 set TERM_SESSION_ID,
	// which is stable for the life of a terminal tab.
	if sid := os.Getenv("TERM_SESSION_ID"); sid != "" {
		return "session:" + sid
	}

	// Strategy 3: WINDOWID from X11 terminals.
	if wid := os.Getenv("WINDOWID"); wid != "" {
		return "window:" + wid
	}

	// Strategy 4: Hash of parent PID — not great (changes on shell restart)
	// but better than nothing.
	ppid := os.Getppid()
	h := sha256.Sum256([]byte(fmt.Sprintf("ppid:%d", ppid)))
	return fmt.Sprintf("ppid:%x", h[:8])
}

// ttyname returns the TTY device name for stdin using Ttyname syscall
// equivalent. We use fstat to get the device number and then construct
// the path.
func ttyname() (string, error) {
	fd := int(os.Stdin.Fd())

	// Check if stdin is a terminal
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", err
	}

	// On macOS/Linux, we can use the /dev/fd symlink to find the tty name
	// or read /proc/self/fd/0 on Linux.
	// The simplest cross-platform approach: readlink on /dev/fd/0
	link, err := os.Readlink("/dev/fd/0")
	if err == nil && link != "" {
		return link, nil
	}

	// Fallback: try /proc/self/fd/0 (Linux)
	link, err = os.Readlink("/proc/self/fd/0")
	if err == nil && link != "" {
		return link, nil
	}

	return "", fmt.Errorf("unable to determine tty name")
}

var slotFileMu sync.Mutex

// loadSlotMappings reads the terminal-slots.json file.
func loadSlotMappings() (map[string]slotMapping, error) {
	data, err := os.ReadFile(terminalSlotsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]slotMapping), nil
		}
		return nil, err
	}

	var mappings map[string]slotMapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		// Corrupt file — start fresh
		return make(map[string]slotMapping), nil
	}
	return mappings, nil
}

// saveSlotMappings writes the terminal-slots.json file.
func saveSlotMappings(mappings map[string]slotMapping) error {
	dir := filepath.Dir(terminalSlotsFile())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(terminalSlotsFile(), data, 0644)
}

// PreviousSlotName looks up the slot name previously assigned to this terminal.
// Returns empty string if no mapping exists.
func PreviousSlotName() string {
	key := TerminalKey()
	if key == "" {
		return ""
	}

	slotFileMu.Lock()
	defer slotFileMu.Unlock()

	mappings, err := loadSlotMappings()
	if err != nil {
		return ""
	}

	if m, ok := mappings[key]; ok {
		return m.SlotName
	}
	return ""
}

// SaveSlotName persists the mapping from the current terminal to a slot name.
// Called after successful registration so that future sessions in the same
// terminal reconnect to the same slot.
func SaveSlotName(name string) {
	key := TerminalKey()
	if key == "" {
		return
	}

	slotFileMu.Lock()
	defer slotFileMu.Unlock()

	mappings, err := loadSlotMappings()
	if err != nil {
		return
	}

	mappings[key] = slotMapping{
		SlotName:  name,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Prune old entries (older than 30 days) to prevent unbounded growth
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for k, v := range mappings {
		if t, err := time.Parse(time.RFC3339, v.UpdatedAt); err == nil && t.Before(cutoff) {
			delete(mappings, k)
		}
	}

	_ = saveSlotMappings(mappings)
}
