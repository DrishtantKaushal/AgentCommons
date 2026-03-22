package wrapper

import (
	"strings"
	"testing"
)

// =============================================================================
// stripANSI tests — verify all ANSI escape code variants are stripped
// =============================================================================

func TestStripANSI_BasicSGR(t *testing.T) {
	// Basic SGR (Select Graphic Rendition): colors, bold, etc.
	input := "\x1b[1m\x1b[36mDo you want to proceed?\x1b[0m"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI basic SGR: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_CursorMovement(t *testing.T) {
	// CSI cursor movement: \x1b[<N>A (up), \x1b[<N>B (down), \x1b[<N>C (right), \x1b[<N>D (left)
	input := "\x1b[5A\x1b[2KDo you want to proceed?\x1b[3B"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI cursor movement: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_DECPrivateMode(t *testing.T) {
	// DEC private modes: \x1b[?25l (hide cursor), \x1b[?25h (show cursor)
	// \x1b[?2026h (synchronized output begin), \x1b[?2026l (synchronized output end)
	// BUG: The current regex \x1b\[[0-9;]*[a-zA-Z] does NOT match these because '?' is not in [0-9;]*
	input := "\x1b[?25l\x1b[?2026hDo you want to proceed?\x1b[?2026l\x1b[?25h"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI DEC private mode: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_EraseSequences(t *testing.T) {
	// Erase line: \x1b[2K (entire line), \x1b[0K (to end), \x1b[1K (to start)
	// Erase display: \x1b[2J (entire screen), \x1b[0J (below)
	input := "\x1b[2K\x1b[0JDo you want to proceed?"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI erase: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_OSCWindowTitle(t *testing.T) {
	// OSC (Operating System Command) for window title: \x1b]0;title\x07
	input := "\x1b]0;Claude Code\x07Do you want to proceed?"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI OSC: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_OSCWithST(t *testing.T) {
	// OSC terminated with ST (String Terminator): \x1b\\
	input := "\x1b]0;Claude Code\x1b\\Do you want to proceed?"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI OSC with ST: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_CursorPosition(t *testing.T) {
	// Cursor position: \x1b[<row>;<col>H
	input := "\x1b[1;1H\x1b[2KDo you want to proceed?"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI cursor position: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_256ColorAndTrueColor(t *testing.T) {
	// 256-color: \x1b[38;5;82m  True-color: \x1b[38;2;255;100;0m
	input := "\x1b[38;5;82mDo you \x1b[38;2;255;100;0mwant to proceed?\x1b[0m"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI extended color: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_InkSpacePadding(t *testing.T) {
	// Ink pads lines with spaces wrapped in reverse-video escape sequences
	input := "\x1b[7m                    \x1b[27m\x1b[1mDo you want to proceed?\x1b[22m\x1b[7m                              \x1b[27m"
	got := stripANSI(input)
	want := "                    Do you want to proceed?                              "
	if got != want {
		t.Errorf("stripANSI Ink padding: got %q, want %q", got, want)
	}
	// After TrimSpace, should match
	if strings.TrimSpace(got) != "Do you want to proceed?" {
		t.Errorf("stripANSI Ink padding trimmed: got %q", strings.TrimSpace(got))
	}
}

func TestStripANSI_AlternateScreenBuffer(t *testing.T) {
	// Alternate screen: \x1b[?1049h (enter), \x1b[?1049l (leave)
	input := "\x1b[?1049hDo you want to proceed?\x1b[?1049l"
	got := stripANSI(input)
	if got != "Do you want to proceed?" {
		t.Errorf("stripANSI alt screen: got %q, want %q", got, "Do you want to proceed?")
	}
}

func TestStripANSI_OSCHyperlink(t *testing.T) {
	// OSC 8 hyperlinks: \x1b]8;;url\x1b\\ text \x1b]8;;\x1b\\
	input := "\x1b]8;;https://example.com\x1b\\Click here\x1b]8;;\x1b\\"
	got := stripANSI(input)
	if got != "Click here" {
		t.Errorf("stripANSI hyperlink: got %q, want %q", got, "Click here")
	}
}

// =============================================================================
// Chunk reassembly tests — pty sends data in arbitrary chunks
// =============================================================================

func TestFeed_PromptSplitAcrossChunks(t *testing.T) {
	// CRITICAL BUG: The pty sends data in arbitrary chunks. The prompt
	// "Do you want to proceed?" might arrive split across two reads:
	//   Read 1: "...some output\nDo you want to "
	//   Read 2: "proceed?\n..."
	// The current detector splits on \n and checks per-line. Since neither
	// chunk contains the complete "Do you want to proceed?" on one line,
	// the regex NEVER matches.
	d := NewDetector()

	// Simulate chunked delivery
	p1 := d.Feed([]byte("Bash command: ls -la\nDo you want to "))
	// First chunk should NOT trigger (incomplete line)
	// But the detector should buffer the partial line.

	p2 := d.Feed([]byte("proceed?\n1. Yes\n"))
	// Second chunk completes the line — should trigger.

	if p1 != nil && p2 != nil {
		// Either approach is fine: detect in chunk 1 or chunk 2
		return
	}
	if p1 == nil && p2 == nil {
		t.Errorf("Feed split across chunks: neither chunk detected the prompt (CRITICAL BUG)")
	}
	// One of them detected it — pass
}

func TestFeed_PromptInSingleChunk(t *testing.T) {
	d := NewDetector()
	p := d.Feed([]byte("Bash command: ls -la\nDo you want to proceed?\n1. Yes\n"))
	if p == nil {
		t.Error("Feed single chunk: expected prompt detection")
	}
	if p != nil && !strings.Contains(p.Text, "Do you want to proceed?") {
		t.Errorf("Feed single chunk: got text %q, want it to contain 'Do you want to proceed?'", p.Text)
	}
}

// =============================================================================
// Realistic Claude Code output tests
// =============================================================================

func TestFeed_RealisticBashApproval(t *testing.T) {
	// Simulate realistic Claude Code Ink output for a bash command approval.
	// Ink re-renders the entire frame: cursor-up, erase-lines, then new content.
	// The actual output includes:
	//  - Synchronized output markers (\x1b[?2026h / \x1b[?2026l)
	//  - Cursor hide/show (\x1b[?25l / \x1b[?25h)
	//  - Cursor movement (\x1b[<N>A to go up, \x1b[<N>G to column)
	//  - Line erase (\x1b[2K)
	//  - SGR color codes (\x1b[1m bold, \x1b[36m cyan, etc.)
	//  - Space padding to terminal width
	//  - \r at end of lines (pty translates \n to \r\n)
	raw := "" +
		"\x1b[?2026h" + // begin synchronized output
		"\x1b[?25l" + // hide cursor
		"\x1b[14A" + // cursor up 14 lines (erase previous frame)
		"\x1b[2K\x1b[1G" + // erase line, cursor to col 1
		"\x1b[1m\x1b[33m  Bash \x1b[0m\x1b[2m command \x1b[0m\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[36mls -la\x1b[0m\r\n" +
		"\x1b[2K\x1b[1G\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[1mDo you want to proceed?\x1b[22m\r\n" +
		"\x1b[2K\x1b[1G\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[36m❯\x1b[0m 1. Yes\r\n" +
		"\x1b[2K\x1b[1G" +
		"    2. Yes, and don't ask again for similar commands in this project\r\n" +
		"\x1b[2K\x1b[1G" +
		"    3. No, and tell Claude what to do differently (esc)\r\n" +
		"\x1b[?25h" + // show cursor
		"\x1b[?2026l" // end synchronized output

	d := NewDetector()
	p := d.Feed([]byte(raw))
	if p == nil {
		t.Error("Feed realistic bash approval: expected prompt detection, got nil")
	}
}

func TestFeed_RealisticMCPToolApproval(t *testing.T) {
	// MCP tool use approval prompt
	raw := "" +
		"\x1b[?25l" +
		"\x1b[8A" +
		"\x1b[2K\x1b[1G" +
		"\x1b[1m\x1b[35m  Tool use \x1b[0m\x1b[2m mcp_server:tool_name \x1b[0m\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[36m{\"arg\": \"value\"}\x1b[0m\r\n" +
		"\x1b[2K\x1b[1G\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[1mDo you want to proceed?\x1b[0m\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[36m❯\x1b[0m 1. Yes\r\n" +
		"\x1b[2K\x1b[1G" +
		"    2. Yes, and don't ask again...\r\n" +
		"\x1b[2K\x1b[1G" +
		"    3. No, and tell Claude what to do differently (esc)\r\n" +
		"\x1b[?25h"

	d := NewDetector()
	p := d.Feed([]byte(raw))
	if p == nil {
		t.Error("Feed realistic MCP tool approval: expected prompt detection, got nil")
	}
}

func TestFeed_RealisticEditApproval(t *testing.T) {
	// File edit approval prompt
	raw := "" +
		"\x1b[?25l" +
		"\x1b[6A" +
		"\x1b[2K\x1b[1G" +
		"\x1b[1m\x1b[32m  Edit file \x1b[0m\x1b[2m src/main.go \x1b[0m\r\n" +
		"\x1b[2K\x1b[1G\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[1mDo you want to proceed?\x1b[0m\r\n" +
		"\x1b[2K\x1b[1G" +
		"  \x1b[36m❯\x1b[0m 1. Yes\r\n" +
		"\x1b[2K\x1b[1G" +
		"    2. Yes, and don't ask again...\r\n" +
		"\x1b[?25h"

	d := NewDetector()
	p := d.Feed([]byte(raw))
	if p == nil {
		t.Error("Feed realistic edit approval: expected prompt detection, got nil")
	}
}

func TestFeed_WithInkSpacePadding(t *testing.T) {
	// Ink pads every line with spaces to the terminal width.
	// This means the actual line might be:
	//   "  Do you want to proceed?                                              "
	// The regex must still match.
	padded := "\x1b[2K\x1b[1G  \x1b[1mDo you want to proceed?\x1b[0m" +
		strings.Repeat(" ", 60) + "\r\n"

	d := NewDetector()
	p := d.Feed([]byte(padded))
	if p == nil {
		t.Error("Feed with Ink space padding: expected prompt detection, got nil")
	}
}

func TestFeed_AllowPromptFormat(t *testing.T) {
	// "Allow" format sometimes used for specific permissions
	raw := "\x1b[1mAllow Claude to read /etc/passwd?\x1b[0m\r\n"

	d := NewDetector()
	p := d.Feed([]byte(raw))
	if p == nil {
		t.Error("Feed 'Allow' prompt: expected prompt detection, got nil")
	}
}

// =============================================================================
// Carriage return handling tests
// =============================================================================

func TestFeed_CarriageReturnOverwrite(t *testing.T) {
	// Ink sometimes uses \r without \n to overwrite the current line.
	// The terminal visually shows only the last write, but the pty output
	// contains both the old and new content on the same "line".
	input := "Loading...\rDo you want to proceed?\r\n"

	d := NewDetector()
	p := d.Feed([]byte(input))
	if p == nil {
		t.Error("Feed carriage return overwrite: expected prompt detection, got nil")
	}
}

func TestFeed_CRLFLineEndings(t *testing.T) {
	// PTY output uses \r\n line endings (pty translates \n to \r\n)
	input := "Bash command: ls\r\nDo you want to proceed?\r\n1. Yes\r\n"

	d := NewDetector()
	p := d.Feed([]byte(input))
	if p == nil {
		t.Error("Feed CRLF: expected prompt detection, got nil")
	}
}

// =============================================================================
// Line buffer accumulation test (the chunk reassembly fix)
// =============================================================================

func TestFeed_LineBufferAccumulation(t *testing.T) {
	// If the detector properly buffers incomplete lines, this should work:
	// Chunk 1 ends mid-line (no trailing \n after the last text)
	// Chunk 2 starts where chunk 1 left off
	d := NewDetector()

	// Chunk 1: ends with an incomplete line "Do you want"
	p1 := d.Feed([]byte("Some header\nDo you want"))
	if p1 != nil {
		// Acceptable if it matches on partial, but unlikely
	}

	// Chunk 2: completes the line " to proceed?\n"
	p2 := d.Feed([]byte(" to proceed?\n1. Yes\n"))

	if p1 == nil && p2 == nil {
		t.Error("Feed line buffer accumulation: prompt split across two Feed() calls was not detected")
	}
}

// =============================================================================
// Negative tests — should NOT match
// =============================================================================

func TestFeed_NoFalsePositiveOnNormalOutput(t *testing.T) {
	d := NewDetector()
	p := d.Feed([]byte("Building project...\nCompiling main.go\nDone.\n"))
	if p != nil {
		t.Errorf("Feed normal output: expected no match, got %q", p.Text)
	}
}

func TestFeed_NoMatchOnPartialText(t *testing.T) {
	// "Do you want" without "to proceed?" should not match
	d := NewDetector()
	p := d.Feed([]byte("Do you want some coffee?\n"))
	if p != nil {
		t.Errorf("Feed partial text: expected no match for 'Do you want some coffee?', got %q", p.Text)
	}
}

// =============================================================================
// Context capture tests
// =============================================================================

func TestFeed_ContextCapture(t *testing.T) {
	d := NewDetector()
	d.Feed([]byte("Thinking...\n"))
	d.Feed([]byte("Bash command: rm -rf /tmp/test\n"))
	p := d.Feed([]byte("Do you want to proceed?\n"))
	if p == nil {
		t.Fatal("Feed context capture: expected prompt detection")
	}
	if !strings.Contains(p.Context, "rm -rf") {
		t.Errorf("Feed context capture: expected context to contain 'rm -rf', got %q", p.Context)
	}
}

// =============================================================================
// Heavy ANSI tests — the "kitchen sink" from real Ink output
// =============================================================================

func TestFeed_KitchenSinkInkOutput(t *testing.T) {
	// This simulates a realistic full Ink frame redraw that a pty would produce.
	// Key sequences present:
	//   \x1b[?2026h  - DEC synchronized output begin
	//   \x1b[?25l    - DEC cursor hide
	//   \x1b[14A     - CSI cursor up 14
	//   \x1b[2K      - CSI erase entire line
	//   \x1b[1G      - CSI cursor to column 1
	//   \x1b[1m      - SGR bold
	//   \x1b[33m     - SGR yellow
	//   \x1b[0m      - SGR reset
	//   \x1b[2m      - SGR dim
	//   \x1b[36m     - SGR cyan
	//   \x1b[22m     - SGR normal intensity
	//   \x1b[35m     - SGR magenta
	//   \x1b[7m      - SGR reverse video (Ink space padding)
	//   \x1b[27m     - SGR reverse video off
	//   \x1b[?25h    - DEC cursor show
	//   \x1b[?2026l  - DEC synchronized output end
	//   \r\n         - CRLF (pty line endings)
	raw := "" +
		"\x1b[?2026h\x1b[?25l" +
		"\x1b[20A" +
		// Ink erases and redraws all lines
		"\x1b[2K\x1b[1G\x1b[7m" + strings.Repeat(" ", 80) + "\x1b[27m\r\n" + // blank padded line
		"\x1b[2K\x1b[1G\x1b[7m  \x1b[27m\x1b[1m\x1b[33m⚡ Bash \x1b[0m\x1b[2m command \x1b[0m\x1b[7m" + strings.Repeat(" ", 60) + "\x1b[27m\r\n" +
		"\x1b[2K\x1b[1G\x1b[7m  \x1b[27m\x1b[36mnpm install express\x1b[0m\x1b[7m" + strings.Repeat(" ", 55) + "\x1b[27m\r\n" +
		"\x1b[2K\x1b[1G\x1b[7m" + strings.Repeat(" ", 80) + "\x1b[27m\r\n" + // blank
		"\x1b[2K\x1b[1G\x1b[7m  \x1b[27m\x1b[2mThis command will install packages.\x1b[22m\x1b[7m" + strings.Repeat(" ", 40) + "\x1b[27m\r\n" +
		"\x1b[2K\x1b[1G\x1b[7m" + strings.Repeat(" ", 80) + "\x1b[27m\r\n" + // blank
		"\x1b[2K\x1b[1G\x1b[7m  \x1b[27m\x1b[1mDo you want to proceed?\x1b[22m\x1b[7m" + strings.Repeat(" ", 52) + "\x1b[27m\r\n" +
		"\x1b[2K\x1b[1G\x1b[7m" + strings.Repeat(" ", 80) + "\x1b[27m\r\n" + // blank
		"\x1b[2K\x1b[1G\x1b[7m  \x1b[27m\x1b[36m❯\x1b[0m 1. Yes\x1b[7m" + strings.Repeat(" ", 67) + "\x1b[27m\r\n" +
		"\x1b[2K\x1b[1G\x1b[7m    \x1b[27m2. Yes, and don't ask again for similar commands in this project\x1b[7m" + strings.Repeat(" ", 10) + "\x1b[27m\r\n" +
		"\x1b[2K\x1b[1G\x1b[7m    \x1b[27m3. No, and tell Claude what to do differently (esc)\x1b[7m" + strings.Repeat(" ", 24) + "\x1b[27m\r\n" +
		"\x1b[?25h\x1b[?2026l"

	d := NewDetector()
	p := d.Feed([]byte(raw))
	if p == nil {
		// Diagnose: let's see what the cleaned text looks like
		cleaned := stripANSI(string(raw))
		t.Errorf("Feed kitchen sink Ink output: expected prompt detection, got nil\nCleaned text:\n%s", cleaned)
	}
}

// =============================================================================
// stripANSI comprehensive verification
// =============================================================================

// =============================================================================
// Deduplication tests — Ink redraws should NOT spam approval_requests
// =============================================================================

func TestFeed_DedupFirstDetection(t *testing.T) {
	// First time seeing "Do you want to proceed?" should return a PendingPrompt.
	d := NewDetector()
	p := d.Feed([]byte("Bash command: ls\nDo you want to proceed?\n"))
	if p == nil {
		t.Fatal("first detection: expected PendingPrompt, got nil")
	}
	if !strings.Contains(p.Text, "Do you want to proceed?") {
		t.Errorf("first detection: got text %q, want it to contain 'Do you want to proceed?'", p.Text)
	}
	if p.Hash == "" {
		t.Error("first detection: expected non-empty hash")
	}
}

func TestFeed_DedupSecondDetectionSameText(t *testing.T) {
	// Ink redraws emit the same "Do you want to proceed?" again.
	// The detector must NOT re-fire — returns nil on the duplicate.
	d := NewDetector()

	p1 := d.Feed([]byte("Bash command: ls\nDo you want to proceed?\n"))
	if p1 == nil {
		t.Fatal("dedup same text: first detection should return PendingPrompt")
	}

	// Simulate Ink redraw: same prompt text arrives again
	p2 := d.Feed([]byte("Do you want to proceed?\n"))
	if p2 != nil {
		t.Errorf("dedup same text: second detection should return nil (dedup), got %q", p2.Text)
	}
}

func TestFeed_DedupAfterClearSameTextFiresAgain(t *testing.T) {
	// After Clear(), the same prompt text should fire again because
	// the pending state has been reset (approval was resolved).
	d := NewDetector()

	p1 := d.Feed([]byte("Do you want to proceed?\n"))
	if p1 == nil {
		t.Fatal("dedup after clear: first detection should return PendingPrompt")
	}

	d.Clear()

	// Same text should fire again after Clear()
	p2 := d.Feed([]byte("Do you want to proceed?\n"))
	if p2 == nil {
		t.Error("dedup after clear: same text should fire again after Clear(), got nil")
	}
}

func TestFeed_DedupDifferentPromptFiresWhilePending(t *testing.T) {
	// A different prompt should fire even if another prompt is already pending.
	// This handles the case where Claude moves to a different approval while
	// the first one is still unresolved.
	d := NewDetector()

	p1 := d.Feed([]byte("Do you want to proceed?\n"))
	if p1 == nil {
		t.Fatal("dedup different prompt: first detection should return PendingPrompt")
	}

	// Different prompt text arrives (different hash)
	p2 := d.Feed([]byte("Allow Claude to read /etc/passwd?\n"))
	if p2 == nil {
		t.Error("dedup different prompt: different prompt text should fire even with existing pending, got nil")
	}
	if p2 != nil && p2.Hash == p1.Hash {
		t.Error("dedup different prompt: second prompt should have a different hash from the first")
	}
}

func TestStripANSI_AllSequenceTypes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "CSI basic color",
			input: "\x1b[31mred\x1b[0m",
			want:  "red",
		},
		{
			name:  "CSI 256 color",
			input: "\x1b[38;5;196mred\x1b[0m",
			want:  "red",
		},
		{
			name:  "CSI true color",
			input: "\x1b[38;2;255;0;0mred\x1b[0m",
			want:  "red",
		},
		{
			name:  "CSI cursor up",
			input: "\x1b[5Ahello",
			want:  "hello",
		},
		{
			name:  "CSI cursor down",
			input: "\x1b[3Bhello",
			want:  "hello",
		},
		{
			name:  "CSI cursor forward",
			input: "\x1b[10Chello",
			want:  "hello",
		},
		{
			name:  "CSI cursor back",
			input: "\x1b[2Dhello",
			want:  "hello",
		},
		{
			name:  "CSI cursor position",
			input: "\x1b[10;20Hhello",
			want:  "hello",
		},
		{
			name:  "CSI cursor column absolute",
			input: "\x1b[1Ghello",
			want:  "hello",
		},
		{
			name:  "CSI erase line",
			input: "\x1b[2Khello",
			want:  "hello",
		},
		{
			name:  "CSI erase display",
			input: "\x1b[2Jhello",
			want:  "hello",
		},
		{
			name:  "CSI scroll up",
			input: "\x1b[2Shello",
			want:  "hello",
		},
		{
			name:  "DEC private mode - hide cursor",
			input: "\x1b[?25lhello\x1b[?25h",
			want:  "hello",
		},
		{
			name:  "DEC private mode - synced output",
			input: "\x1b[?2026hhello\x1b[?2026l",
			want:  "hello",
		},
		{
			name:  "DEC private mode - alt screen",
			input: "\x1b[?1049hhello\x1b[?1049l",
			want:  "hello",
		},
		{
			name:  "OSC with BEL",
			input: "\x1b]0;Window Title\x07hello",
			want:  "hello",
		},
		{
			name:  "OSC with ST",
			input: "\x1b]0;Window Title\x1b\\hello",
			want:  "hello",
		},
		{
			name:  "OSC 8 hyperlink",
			input: "\x1b]8;;https://example.com\x07link\x1b]8;;\x07",
			want:  "link",
		},
		{
			name:  "mixed sequences",
			input: "\x1b[?25l\x1b[5A\x1b[2K\x1b[1G\x1b[1m\x1b[36mhello world\x1b[0m\x1b[?25h",
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
