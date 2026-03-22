package wrapper

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PendingPrompt holds the current pending approval prompt state.
type PendingPrompt struct {
	Text       string
	Hash       string
	Context    string
	DetectedAt time.Time
}

// Detector scans pty output for Claude Code approval prompts.
//
// Claude Code uses Ink (React for terminals) which renders via full-screen
// redraws. The raw pty output contains:
//   - DEC private mode sequences: \x1b[?25l (hide cursor), \x1b[?2026h (sync output)
//   - CSI cursor movement: \x1b[14A (up), \x1b[1G (column), \x1b[2K (erase line)
//   - SGR styling: \x1b[1m (bold), \x1b[36m (cyan), etc.
//   - Ink space-padding with reverse video: \x1b[7m<spaces>\x1b[27m
//   - \r\n line endings (pty translates \n to \r\n)
//   - Data arriving in ARBITRARY CHUNKS — a single prompt line may be split
//     across multiple pty reads.
//
// The detector must handle all of these to reliably find "Do you want to proceed?".
type Detector struct {
	patterns []*regexp.Regexp
	mu       sync.Mutex
	pending  *PendingPrompt
	lines    []string // rolling context buffer
	maxCtx   int
	partial  string // incomplete line buffer for cross-chunk reassembly
}

// NewDetector creates a detector with the default Claude Code approval patterns.
func NewDetector() *Detector {
	return &Detector{
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)Allow .+\?`),
			regexp.MustCompile(`(?i)Do you want to proceed\?`),
		},
		maxCtx: 5,
	}
}

// Feed processes a byte chunk from the pty, extracting complete lines and
// checking for approval prompt matches.
//
// Pty reads deliver data in arbitrary chunks. A single logical line like
// "Do you want to proceed?" may arrive split across two Feed() calls:
//
//	Feed([]byte("...header\nDo you want to "))  // no trailing \n
//	Feed([]byte("proceed?\n1. Yes\n"))           // completes the line
//
// The detector buffers the trailing incomplete fragment (no terminating \n)
// and prepends it to the next chunk so the regex can match the full line.
//
// Returns a *PendingPrompt if a new match was detected, nil otherwise.
func (d *Detector) Feed(data []byte) *PendingPrompt {
	text := string(data)

	// Prepend any buffered partial line from the previous chunk.
	d.mu.Lock()
	if d.partial != "" {
		text = d.partial + text
		d.partial = ""
	}
	d.mu.Unlock()

	// Strip ANSI escape codes for pattern matching.
	cleaned := stripANSI(text)

	// Handle \r-only "lines" (Ink sometimes uses \r without \n to overwrite).
	// Replace standalone \r (not followed by \n) with \n, preserving the char after \r.
	cleaned = crOnlyRegex.ReplaceAllString(cleaned, "\n$1")

	// Split into lines. The last element may be an incomplete line if the
	// chunk didn't end with \n — buffer it for the next Feed() call.
	rawLines := strings.Split(cleaned, "\n")

	// If the chunk did NOT end with \n, the last element is a partial line.
	// Buffer it and exclude from processing.
	if len(cleaned) > 0 && cleaned[len(cleaned)-1] != '\n' {
		d.mu.Lock()
		d.partial = rawLines[len(rawLines)-1]
		d.mu.Unlock()
		rawLines = rawLines[:len(rawLines)-1]
	}

	for _, line := range rawLines {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Add to context buffer
		d.mu.Lock()
		d.lines = append(d.lines, line)
		if len(d.lines) > d.maxCtx {
			d.lines = d.lines[len(d.lines)-d.maxCtx:]
		}
		d.mu.Unlock()

		// Check patterns
		for _, pat := range d.patterns {
			if pat.MatchString(line) {
				// Check if this is a self-initiated prompt (e.g., "commons approve" bash command)
				if d.isSelfInitiated() {
					continue // skip this match
				}

				hash := sha256Hex(line)

				// Don't re-fire if we already have a pending prompt with the same hash.
				// Ink redraws the entire screen periodically, re-emitting "Do you want
				// to proceed?" on each redraw. Without this guard, every redraw triggers
				// a new approval_request broadcast, spamming the orchestrating terminal.
				d.mu.Lock()
				if d.pending != nil && d.pending.Hash == hash {
					d.mu.Unlock()
					continue // same prompt already pending, skip
				}
				d.mu.Unlock()

				ctx := d.getContext()

				prompt := &PendingPrompt{
					Text:       line,
					Hash:       hash,
					Context:    ctx,
					DetectedAt: time.Now(),
				}

				d.mu.Lock()
				d.pending = prompt
				d.mu.Unlock()

				return prompt
			}
		}
	}

	return nil
}

// Current returns the current pending prompt (if any).
func (d *Detector) Current() *PendingPrompt {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pending
}

// Clear clears the current pending prompt.
func (d *Detector) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = nil
}

// isSelfInitiated checks whether the current approval prompt was triggered by
// a commons CLI command (e.g., "commons approve", "commons deny", "commons push").
// These commands naturally produce their own approval prompts when run inside
// Claude Code, and we should not broadcast those as organic approvals.
func (d *Detector) isSelfInitiated() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check the last few context lines for commons CLI command patterns
	selfPatterns := []string{
		"commons approve",
		"commons deny",
		"commons push",
		"commons status",
		"commons_approve",
		"commons_deny",
		"commons_push",
	}

	for _, ctxLine := range d.lines {
		lower := strings.ToLower(ctxLine)
		for _, pat := range selfPatterns {
			if strings.Contains(lower, pat) {
				return true
			}
		}
	}
	return false
}

func (d *Detector) getContext() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lines) <= 1 {
		return ""
	}
	// Return lines before the current match
	ctx := d.lines[:len(d.lines)-1]
	return strings.Join(ctx, "\n")
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// ansiRegex matches ALL ANSI escape sequences that Claude Code / Ink can produce:
//
//  1. CSI (Control Sequence Introducer) sequences: \x1b[ ... <letter>
//     Standard:  \x1b[<digits/semicolons><letter>   e.g. \x1b[31m, \x1b[2K, \x1b[14A
//     DEC private: \x1b[?<digits/semicolons><letter>  e.g. \x1b[?25l, \x1b[?2026h, \x1b[?1049h
//     The '?' after '[' distinguishes DEC private modes. The old regex lacked '?'.
//
//  2. OSC (Operating System Command) sequences: \x1b] ... (BEL | ST)
//     e.g. \x1b]0;Window Title\x07  or  \x1b]8;;url\x1b\\
//
// The order matters: CSI with '?' must be tried, so we use [?]? in the CSI branch.
var ansiRegex = regexp.MustCompile(`\x1b\[[?]?[0-9;]*[a-zA-Z]|\x1b\].*?(\x07|\x1b\\)`)

// cursorForwardRegex matches \x1b[C or \x1b[NC where N is the count.
// Ink uses \x1b[1C instead of space characters between words.
// We replace these with actual spaces before stripping other ANSI codes.
var cursorForwardRegex = regexp.MustCompile(`\x1b\[(\d*)C`)

// crOnlyRegex matches \r that is NOT followed by \n — standalone carriage returns
// used by Ink to overwrite lines in place.
var crOnlyRegex = regexp.MustCompile(`\r([^\n])`)

func stripANSI(s string) string {
	// First: replace cursor-forward sequences with spaces.
	// Ink uses \x1b[1C (cursor forward 1) instead of space between words.
	// \x1b[C = forward 1, \x1b[3C = forward 3, etc.
	s = cursorForwardRegex.ReplaceAllStringFunc(s, func(match string) string {
		sub := cursorForwardRegex.FindStringSubmatch(match)
		n := 1
		if len(sub) > 1 && sub[1] != "" {
			fmt.Sscanf(sub[1], "%d", &n)
		}
		return strings.Repeat(" ", n)
	})

	// Then strip all remaining ANSI sequences
	return ansiRegex.ReplaceAllString(s, "")
}
