package wrapper

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

// startCaffeinate prevents idle sleep while the session is active.
// On macOS: caffeinate -i -w $PID
// On Linux: systemd-inhibit
func startCaffeinate() *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		pid := strconv.Itoa(os.Getpid())
		cmd := exec.Command("caffeinate", "-i", "-w", pid)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			log.Printf("[wrapper] caffeinate start failed: %v", err)
			return nil
		}
		log.Printf("[wrapper] caffeinate started (watching pid %s)", pid)
		return cmd

	case "linux":
		cmd := exec.Command("systemd-inhibit",
			"--what=idle",
			"--who=commons",
			"--why=agent session active",
			"--", "sleep", "infinity",
		)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			log.Printf("[wrapper] systemd-inhibit start failed: %v", err)
			return nil
		}
		return cmd

	default:
		log.Printf("[wrapper] no sleep prevention available for %s", runtime.GOOS)
		return nil
	}
}

func stopCaffeinate(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil {
		// Process may have already exited (e.g., caffeinate -w exited when parent died)
		_ = fmt.Errorf("kill caffeinate: %w", err)
	}
	cmd.Wait()
}
