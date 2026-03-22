package naming

import "os"

// cursorTerminalTitle attempts to read the Cursor terminal tab name.
// This is a best-effort detection — returns empty string if unavailable.
func cursorTerminalTitle() string {
	// Cursor sets TERM_PROGRAM=vscode when running in its terminal.
	// The actual terminal tab name is stored in Cursor's workspace state DB,
	// which requires SQLite access and workspace path detection.
	// For R1, we use a simpler approach: check environment variables
	// that Cursor might set, or fall back to empty.

	// Check if the user set COMMONS_SLOT_NAME explicitly
	if name := os.Getenv("COMMONS_SLOT_NAME"); name != "" {
		return name
	}

	// TODO: Implement Cursor state.vscdb reading for terminal tab names
	// This requires finding the Cursor workspace storage path and querying
	// the SQLite database for terminal title data.

	return ""
}
