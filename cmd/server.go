package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DrishtantKaushal/AgentCommons/internal/config"
	"github.com/DrishtantKaushal/AgentCommons/internal/daemon"
	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/spf13/cobra"
)

func ServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the commons daemon",
	}

	cmd.AddCommand(serverStartCmd())
	cmd.AddCommand(serverStopCmd())
	cmd.AddCommand(serverStatusCmd())

	return cmd
}

func serverStartCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the commons daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()

			if foreground {
				return daemon.Run(cfg)
			}

			// Check if already running
			if pid := readDaemonPID(); pid > 0 && isProcAlive(pid) {
				fmt.Printf("Commons daemon already running (pid %d)\n", pid)
				return nil
			}

			// Fork/exec the daemon as a detached background process
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("find executable: %w", err)
			}

			child := exec.Command(exe, "server", "start", "--foreground")
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			child.Stdout = nil
			child.Stderr = nil
			child.Stdin = nil

			if err := child.Start(); err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}

			childPid := child.Process.Pid

			// Detach — don't wait for child
			child.Process.Release()

			// Poll /health for up to 6 seconds
			addr := daemon.Addr(cfg.Port)
			healthURL := fmt.Sprintf("http://%s/health", addr)

			for i := 0; i < 12; i++ {
				time.Sleep(500 * time.Millisecond)
				resp, err := http.Get(healthURL)
				if err == nil {
					resp.Body.Close()
					fmt.Printf("Commons daemon started (pid %d)\n", childPid)
					return nil
				}
			}

			return fmt.Errorf("daemon started but health check failed after 6s")
		},
	}

	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in foreground (used internally)")
	cmd.Flags().MarkHidden("foreground")

	return cmd
}

func serverStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running commons daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid := readDaemonPID()
			if pid <= 0 {
				fmt.Println("Commons daemon is not running")
				return nil
			}

			if !isProcAlive(pid) {
				fmt.Println("Commons daemon is not running (stale PID file)")
				os.Remove(db.CommonsDir() + "/daemon.pid")
				return nil
			}

			// Send SIGTERM
			proc, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("find process: %w", err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("send SIGTERM: %w", err)
			}

			// Wait for exit (up to 5s)
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				if !isProcAlive(pid) {
					fmt.Println("Commons daemon stopped")
					os.Remove(db.CommonsDir() + "/daemon.pid")
					return nil
				}
			}

			return fmt.Errorf("daemon did not stop within 5s (pid %d)", pid)
		},
	}
}

func serverStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon health, uptime, and connected agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			addr := daemon.Addr(cfg.Port)
			healthURL := fmt.Sprintf("http://%s/health", addr)

			resp, err := http.Get(healthURL)
			if err != nil {
				fmt.Println("Commons daemon: not running")
				return nil
			}
			defer resp.Body.Close()

			var health map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&health)

			pid := readDaemonPID()
			fmt.Printf("Commons daemon: running (pid %d, uptime %s)\n", pid, health["uptime"])
			fmt.Printf("Connected agents: %.0f\n", health["connected_agents"])

			dbPath := db.DBPath()
			if info, err := os.Stat(dbPath); err == nil {
				fmt.Printf("Database: %s (%.1f KB)\n", dbPath, float64(info.Size())/1024)
			}

			return nil
		},
	}
}

func readDaemonPID() int {
	data, err := os.ReadFile(db.CommonsDir() + "/daemon.pid")
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

func isProcAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
