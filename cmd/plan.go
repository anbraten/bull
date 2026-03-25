package cmd

import (
	"github.com/anbraten/bull/internal/engine"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan [file]",
	Short: "Preview what changes would be applied",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := resolveFile(args)
		verbose := cmd.Flags().Lookup("verbose").Value.String() == "true"
		eng := engine.New(verbose)
		return eng.Plan(file)
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
	planCmd.Flags().BoolP("verbose", "v", false, "Enable verbose output")
}

func resolveFile(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "infra.lua"
}
