package naming

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// wrapperSession records which session_id is actively running in a given terminal.
type wrapperSession struct {
	SessionID string `json:"session_id"`
	UpdatedAt string `json:"updated_at"`
}

// wrapperSessionsFile returns the path to ~/.commons/wrapper-sessions.json.
func wrapperSessionsFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".commons", "wrapper-sessions.json")
}

// loadWrapperSessions reads the wrapper-sessions.json file, tolerating missing files.
func loadWrapperSessions() (map[string]wrapperSession, error) {
	data, err := os.ReadFile(wrapperSessionsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]wrapperSession), nil
		}
		return nil, err
	}

	var sessions map[string]wrapperSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		// Corrupt file — start fresh
		return make(map[string]wrapperSession), nil
	}
	return sessions, nil
}

// saveWrapperSessions writes the wrapper-sessions.json file.
func saveWrapperSessions(sessions map[string]wrapperSession) error {
	dir := filepath.Dir(wrapperSessionsFile())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(wrapperSessionsFile(), data, 0644)
}

// SaveWrapperSession records the active wrapper session_id for the current terminal.
func SaveWrapperSession(sessionID string) {
	key := TerminalKey()
	if key == "" {
		return
	}

	slotFileMu.Lock()
	defer slotFileMu.Unlock()

	sessions, err := loadWrapperSessions()
	if err != nil {
		return
	}

	sessions[key] = wrapperSession{
		SessionID: sessionID,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	_ = saveWrapperSessions(sessions)
}

// LoadWrapperSession returns the session_id of the active wrapper in the current
// terminal, or empty string if none or stale (older than 5 minutes).
func LoadWrapperSession() string {
	key := TerminalKey()
	if key == "" {
		return ""
	}

	slotFileMu.Lock()
	defer slotFileMu.Unlock()

	sessions, err := loadWrapperSessions()
	if err != nil {
		return ""
	}

	ws, ok := sessions[key]
	if !ok {
		return ""
	}

	// Treat entries older than 5 minutes as stale
	if t, err := time.Parse(time.RFC3339, ws.UpdatedAt); err == nil {
		if time.Since(t) > 5*time.Minute {
			return ""
		}
	}

	return ws.SessionID
}

// ClearWrapperSession removes the wrapper session entry for the current terminal.
func ClearWrapperSession() {
	key := TerminalKey()
	if key == "" {
		return
	}

	slotFileMu.Lock()
	defer slotFileMu.Unlock()

	sessions, err := loadWrapperSessions()
	if err != nil {
		return
	}

	delete(sessions, key)
	_ = saveWrapperSessions(sessions)
}
