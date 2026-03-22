package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/DrishtantKaushal/AgentCommons/internal/db"
	"github.com/spf13/cobra"
)

func InstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "One-time setup: create ~/.commons/, init DB, configure MCP",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("commons v0.1.0")
			fmt.Println()

			// Check prerequisites
			fmt.Println("Checking prerequisites...")
			if claudePath, err := exec.LookPath("claude"); err == nil {
				fmt.Printf("  [check] claude found at %s\n", claudePath)
			} else {
				fmt.Println("  [warn] claude not found on $PATH (optional for MCP-only mode)")
			}
			if commonsPath, err := exec.LookPath("commons"); err == nil {
				fmt.Printf("  [check] commons binary on $PATH (%s)\n", commonsPath)
			} else {
				fmt.Println("  [warn] commons not on $PATH — add it for global access")
			}
			fmt.Println()

			// Create data directory
			commonsDir := db.CommonsDir()
			fmt.Println("Creating " + commonsDir + "/ ...")

			// Initialize DB
			database, err := db.Open()
			if err != nil {
				return fmt.Errorf("initialize database: %w", err)
			}
			database.Close()
			fmt.Printf("  [check] %s initialized (SQLite, WAL mode)\n", db.DBPath())

			// Write config.toml
			configPath := filepath.Join(commonsDir, "config.toml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				configContent := `# Commons daemon configuration
[daemon]
port = 7390
heartbeat_interval = "10s"
heartbeat_timeout = "30s"
reaper_interval = "10s"
grace_period = "60s"

[notifications]
default = "direct"
`
				os.WriteFile(configPath, []byte(configContent), 0644)
			}
			fmt.Printf("  [check] %s written (defaults)\n", configPath)

			// Write approval-patterns.yaml
			patternsPath := filepath.Join(commonsDir, "approval-patterns.yaml")
			if _, err := os.Stat(patternsPath); os.IsNotExist(err) {
				patternsContent := `# Approval patterns for Claude Code
patterns:
  - regex: 'Allow .+\?'
    approve_key: "y"
    deny_key: "n"
  - regex: 'Do you want to proceed\?'
    approve_key: "y"
    deny_key: "n"
`
				os.WriteFile(patternsPath, []byte(patternsContent), 0644)
			}
			fmt.Printf("  [check] %s written (Claude Code patterns)\n", patternsPath)
			fmt.Println()

			// Copy plugin to ~/.commons/plugin/
			if err := copyPlugin(commonsDir); err != nil {
				fmt.Printf("  [warn] Plugin copy: %v\n", err)
			} else {
				fmt.Printf("  [check] Commons plugin installed at %s/plugin/\n", commonsDir)
			}
			fmt.Println()

			// Configure MCP server in Claude Code settings
			fmt.Println("Configuring Claude Code MCP integration...")
			if err := configureMCP(); err != nil {
				fmt.Printf("  [warn] MCP config: %v\n", err)
			} else {
				home, _ := os.UserHomeDir()
				fmt.Printf("  [check] Added \"commons\" MCP server to %s/.claude/settings.local.json\n", home)
			}

			fmt.Println()
			fmt.Println("Setup complete.")
			return nil
		},
	}
}

func copyPlugin(commonsDir string) error {
	// Find the plugin source relative to the executable
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exePath)
	srcPlugin := ""
	for _, c := range []string{
		filepath.Join(exeDir, "plugin"),
		filepath.Join(exeDir, "..", "plugin"),
	} {
		if _, err := os.Stat(filepath.Join(c, ".claude-plugin", "plugin.json")); err == nil {
			srcPlugin = c
			break
		}
	}
	if srcPlugin == "" {
		return fmt.Errorf("plugin source not found relative to binary")
	}

	// Copy plugin tree to ~/.commons/plugin/
	dstPlugin := filepath.Join(commonsDir, "plugin")
	os.RemoveAll(dstPlugin)
	return copyDir(srcPlugin, dstPlugin)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, 0644)
	})
}

func configureMCP() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	settingsPath := filepath.Join(home, ".claude", "settings.local.json")

	// Ensure .claude directory exists
	os.MkdirAll(filepath.Dir(settingsPath), 0755)

	// Read existing settings or create new
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		settings = make(map[string]interface{})
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	}

	// Get or create mcpServers
	mcpServers, ok := settings["mcpServers"].(map[string]interface{})
	if !ok {
		mcpServers = make(map[string]interface{})
	}

	// Find the MCP server JS path (absolute)
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	// Look for the MCP dist relative to the binary or source tree
	mcpJSPath := ""
	candidates := []string{
		filepath.Join(exeDir, "mcp", "dist", "index.js"),
		filepath.Join(exeDir, "..", "mcp", "dist", "index.js"),
		filepath.Join(exeDir, "..", "src", "mcp", "dist", "index.js"),
	}
	for _, c := range candidates {
		if abs, absErr := filepath.Abs(c); absErr == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				mcpJSPath = abs
				break
			}
		}
	}

	if mcpJSPath == "" {
		return fmt.Errorf("MCP server JS not found. Run: cd src/mcp && npm run build")
	}

	// Write the MCP path to ~/.commons/mcp-path for the mcp-server command
	os.WriteFile(filepath.Join(db.CommonsDir(), "mcp-path"), []byte(mcpJSPath), 0644)

	// Use node directly in the MCP config — more reliable than going through commons binary
	nodePath, nodeErr := exec.LookPath("node")
	if nodeErr != nil {
		return fmt.Errorf("node not found on $PATH")
	}

	// Add/update commons MCP server entry
	mcpServers["commons"] = map[string]interface{}{
		"command": nodePath,
		"args":    []string{mcpJSPath},
	}

	settings["mcpServers"] = mcpServers

	// Write back
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(settingsPath, out, 0644)
}
