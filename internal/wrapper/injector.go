package wrapper

import (
	"log"
	"os"
)

// Injector handles writing approval/denial keystrokes to the pty master.
type Injector struct {
	masterFd *os.File
	detector *Detector
}

// NewInjector creates an injector that writes to the pty master fd.
func NewInjector(masterFd *os.File, detector *Detector) *Injector {
	return &Injector{
		masterFd: masterFd,
		detector: detector,
	}
}

// Approve injects the approve keystroke if the prompt hash matches.
// Claude Code's permission prompt is a numbered list selector:
//   > 1. Yes
//     2. Yes, allow reading from X/ from this project
//     3. No
// The default selection is "1. Yes". Pressing Enter (\r) confirms it.
// Returns true if injected, false if hash mismatch.
func (inj *Injector) Approve(promptHash string) bool {
	pending := inj.detector.Current()
	if pending == nil {
		log.Printf("[injector] no pending prompt")
		return false
	}

	if pending.Hash != promptHash {
		log.Printf("[injector] prompt hash mismatch: expected %s, got %s", pending.Hash, promptHash)
		return false
	}

	// Write '1' to instantly select option "1. Yes" via ink-select-input's
	// number-key instant selection. Claude Code's "Do you want to proceed?"
	// is an Ink SelectInput component that accepts number keys directly.
	if _, err := inj.masterFd.Write([]byte("1")); err != nil {
		log.Printf("[injector] write approve failed: %v", err)
		return false
	}

	inj.detector.Clear()
	log.Printf("[injector] approved: %s", pending.Text)
	return true
}

// Deny injects the deny keystroke if the prompt hash matches.
// Navigates to option 3 (No) using two down-arrow presses, then Enter.
// Returns true if injected, false if hash mismatch.
func (inj *Injector) Deny(promptHash string) bool {
	pending := inj.detector.Current()
	if pending == nil {
		log.Printf("[injector] no pending prompt for denial")
		return false
	}

	if pending.Hash != promptHash {
		log.Printf("[injector] prompt hash mismatch on deny: expected %s, got %s", pending.Hash, promptHash)
		return false
	}

	// Write '3' to instantly select option "3. No" via ink-select-input's
	// number-key instant selection.
	if _, err := inj.masterFd.Write([]byte("3")); err != nil {
		log.Printf("[injector] write deny failed: %v", err)
		return false
	}

	inj.detector.Clear()
	log.Printf("[injector] denied: %s", pending.Text)
	return true
}
