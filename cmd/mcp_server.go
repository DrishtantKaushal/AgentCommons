package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/spf13/cobra"
)

func McpServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "mcp-server",
		Short:  "Start the MCP server (called by Claude Code, not by users)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Find the MCP server's built JS file
			mcpPath := findMCPServer()
			if mcpPath == "" {
				return fmt.Errorf("MCP server not found. Run: cd src/mcp && npm run build")
			}

			// Exec node with the MCP server
			nodePath, err := exec.LookPath("node")
			if err != nil {
				return fmt.Errorf("node not found on $PATH")
			}

			env := os.Environ()
			return execSyscall(nodePath, []string{"node", mcpPath}, env)
		},
	}
}

func findMCPServer() string {
	// 1. Check COMMONS_MCP_PATH env var (set by install)
	if p := os.Getenv("COMMONS_MCP_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 2. Check ~/.commons/mcp-path (written by install)
	pathFile := filepath.Join(db.CommonsDir(), "mcp-path")
	if data, err := os.ReadFile(pathFile); err == nil {
		p := string(data)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 3. Try relative to the executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "mcp", "dist", "index.js"),
			filepath.Join(exeDir, "..", "mcp", "dist", "index.js"),
		}
		for _, c := range candidates {
			if abs, err := filepath.Abs(c); err == nil {
				if _, err := os.Stat(abs); err == nil {
					return abs
				}
			}
		}
	}

	return ""
}

// execSyscall replaces the current process with the given command.
func execSyscall(path string, args []string, env []string) error {
	return execve(path, args, env)
}
