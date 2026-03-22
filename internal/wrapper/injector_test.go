package wrapper

import (
	"testing"
	"time"

	ptyPkg "github.com/creack/pty/v2"
)

// =============================================================================
// Injector tests — verify approval/denial keystrokes via pty
//
// HISTORY: The CLAUDE.md "Approval detection and injection" section says
// "write `y` to pty master fd". That was the OLD behavior when Claude Code
// used a simple y/n confirmation prompt. Claude Code now renders a numbered
// list selector (Ink SelectInput component):
//
//   > 1. Yes
//     2. Yes, allow reading from X/ from this project
//     3. No
//
// The current injector writes '1' (byte 0x31) for approve and '3' (byte 0x33)
// for deny, relying on ink-select-input's instant number-key selection. This
// is based on research in research/numbered-input-selection.md which found
// that Claude Code's Ink SelectInput component supports pressing a number key
// to instantly select that option. If Claude Code ever changes this behavior,
// these tests will need updating.
// =============================================================================

// TestInjectorApprove verifies that Approve writes '1' (byte 49) to the pty.
func TestInjectorApprove(t *testing.T) {
	// Create a pty pair: master (ptmx) and slave (tty).
	ptmx, tty, err := ptyPkg.Open()
	if err != nil {
		t.Fatalf("pty.Open failed: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	// Create a detector and feed it a prompt so there's a pending prompt.
	detector := NewDetector()
	prompt := detector.Feed([]byte("Bash command: ls\nDo you want to proceed?\n1. Yes\n"))
	if prompt == nil {
		t.Fatal("detector did not detect the prompt")
	}

	// Create injector wired to the pty master.
	injector := NewInjector(ptmx, detector)

	// Approve with the correct hash.
	ok := injector.Approve(prompt.Hash)
	if !ok {
		t.Fatal("Approve returned false")
	}

	// Read from the master side of the pty to verify what was written.
	// The injector writes to ptmx (master). In default (cooked) pty mode,
	// characters written to the master are echoed back and can be read from
	// the master. We read the echo to verify the injected byte.
	buf := make([]byte, 16)
	ptmx.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := ptmx.Read(buf)
	if err != nil {
		t.Fatalf("read from pty master (echo) failed: %v", err)
	}

	// Expect exactly '1' (byte 49 / 0x31) — the number key for "1. Yes".
	// NOT 'y' (the old behavior documented in CLAUDE.md).
	if n != 1 {
		t.Errorf("expected 1 byte, got %d bytes: %v", n, buf[:n])
	}
	if buf[0] != '1' {
		t.Errorf("expected byte '1' (0x31 / 49), got byte %d (0x%02x / %q)", buf[0], buf[0], string(buf[:1]))
	}

	// Verify the detector was cleared after approval.
	if detector.Current() != nil {
		t.Error("expected detector to be cleared after Approve")
	}
}

// TestInjectorDeny verifies that Deny writes '3' (byte 51) to the pty.
func TestInjectorDeny(t *testing.T) {
	// Create a pty pair.
	ptmx, tty, err := ptyPkg.Open()
	if err != nil {
		t.Fatalf("pty.Open failed: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	// Create a detector and feed it a prompt.
	detector := NewDetector()
	prompt := detector.Feed([]byte("Bash command: rm -rf /\nDo you want to proceed?\n1. Yes\n"))
	if prompt == nil {
		t.Fatal("detector did not detect the prompt")
	}

	// Create injector wired to the pty master.
	injector := NewInjector(ptmx, detector)

	// Deny with the correct hash.
	ok := injector.Deny(prompt.Hash)
	if !ok {
		t.Fatal("Deny returned false")
	}

	// Read from the master side (echo in cooked pty mode).
	buf := make([]byte, 16)
	ptmx.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := ptmx.Read(buf)
	if err != nil {
		t.Fatalf("read from pty master (echo) failed: %v", err)
	}

	// Expect exactly '3' (byte 51 / 0x33) — the number key for "3. No".
	// NOT arrow-down sequences or 'n' (hypothetical old behavior).
	if n != 1 {
		t.Errorf("expected 1 byte, got %d bytes: %v", n, buf[:n])
	}
	if buf[0] != '3' {
		t.Errorf("expected byte '3' (0x33 / 51), got byte %d (0x%02x / %q)", buf[0], buf[0], string(buf[:1]))
	}

	// Verify the detector was cleared after denial.
	if detector.Current() != nil {
		t.Error("expected detector to be cleared after Deny")
	}
}

// TestInjectorApproveHashMismatch verifies that Approve rejects a wrong hash.
func TestInjectorApproveHashMismatch(t *testing.T) {
	ptmx, tty, err := ptyPkg.Open()
	if err != nil {
		t.Fatalf("pty.Open failed: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	detector := NewDetector()
	prompt := detector.Feed([]byte("Do you want to proceed?\n"))
	if prompt == nil {
		t.Fatal("detector did not detect the prompt")
	}

	injector := NewInjector(ptmx, detector)

	// Use a wrong hash — should return false and write nothing.
	ok := injector.Approve("wrong-hash-value")
	if ok {
		t.Error("Approve should return false on hash mismatch")
	}

	// Verify pending prompt was NOT cleared.
	if detector.Current() == nil {
		t.Error("detector should still have a pending prompt after hash mismatch")
	}
}

// TestInjectorDenyNoPending verifies that Deny returns false when no prompt is pending.
func TestInjectorDenyNoPending(t *testing.T) {
	ptmx, tty, err := ptyPkg.Open()
	if err != nil {
		t.Fatalf("pty.Open failed: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	detector := NewDetector()
	injector := NewInjector(ptmx, detector)

	// No prompt has been fed — Deny should fail.
	ok := injector.Deny("some-hash")
	if ok {
		t.Error("Deny should return false when no prompt is pending")
	}
}
