package cmd

import (
	"github.com/DrishtantKaushal/AgentCommons/internal/wrapper"
	"github.com/spf13/cobra"
)

func RunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [tool]",
		Short: "Run an agent tool with commons session wrapping",
		Long:  "Wraps an agent CLI (e.g., claude) in a pty for approval detection and injection.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			tool := args[0]
			return wrapper.Run(tool, name)
		},
	}

	cmd.Flags().String("name", "", "Override the auto-assigned terminal name")

	return cmd
}
