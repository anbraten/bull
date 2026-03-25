package cmd

import (
	"github.com/anbraten/bull/internal/engine"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate the configuration without applying changes",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := resolveFile(args)
		eng := engine.New(false)
		return eng.Validate(file)
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
