package naming

import (
	"os"
	"strings"
)

// Resolve returns a terminal name using a 3-layer resolution:
// 0. Previously persisted slot name for this terminal (session resume)
// 1. Cursor terminal title (if available)
// 2. Memorable wordlist name (e.g., "SwiftFalcon", "CobaltSpire")
//
// Note: cwd-based naming ("Src", "Mcp") was removed because it produces
// ugly names and causes collisions when multiple terminals share a directory.
func Resolve() string {
	// Layer 0: Previously persisted slot name for this terminal
	if name := PreviousSlotName(); name != "" {
		return name
	}

	// Layer 1: Cursor terminal title
	if name := cursorTerminalTitle(); name != "" {
		return name
	}

	// Layer 2: Memorable wordlist name
	return WordlistName()
}

// fromCWD generates a PascalCase name from the cwd basename.
func fromCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Get basename
	parts := strings.Split(cwd, "/")
	basename := parts[len(parts)-1]

	if basename == "" || basename == "/" || basename == "~" {
		return ""
	}

	return toPascalCase(basename)
}

// toPascalCase converts "backend-api" to "BackendApi", "auth_service" to "AuthService".
func toPascalCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	var result strings.Builder
	for _, w := range words {
		if len(w) > 0 {
			result.WriteString(strings.ToUpper(w[:1]))
			if len(w) > 1 {
				result.WriteString(w[1:])
			}
		}
	}
	if result.Len() == 0 {
		return s
	}
	return result.String()
}
