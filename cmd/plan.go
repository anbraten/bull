package cmd

import (
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan [file]",
	Short: "Preview what changes would be applied",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := resolveFile(args)
		eng, err := newEngine(file)
		if err != nil {
			return err
		}
		return eng.Plan(file)
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
}

func resolveFile(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "infra.lua"
}
