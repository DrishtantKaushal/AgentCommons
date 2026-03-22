package main

import (
	"fmt"
	"os"

	"github.com/DrishtantKaushal/AgentCommons/cmd"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	rootCmd := &cobra.Command{
		Use:   "commons",
		Short: "AgentCommons — coordination layer for your terminal fleet",
		Long:  "AgentCommons orchestrates agent activity across sessions, terminals, and machines. An attention multiplexer.",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	rootCmd.Version = version
	rootCmd.SetVersionTemplate("commons v{{.Version}}\n")

	rootCmd.AddCommand(cmd.ServerCmd())
	rootCmd.AddCommand(cmd.RunCmd())
	rootCmd.AddCommand(cmd.StatusCmd())
	rootCmd.AddCommand(cmd.ApproveCmd())
	rootCmd.AddCommand(cmd.DenyCmd())
	rootCmd.AddCommand(cmd.PushCmd())
	rootCmd.AddCommand(cmd.HistoryCmd())
	rootCmd.AddCommand(cmd.InstallCmd())
	rootCmd.AddCommand(cmd.McpServerCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
