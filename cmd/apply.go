package cmd

import (
	"github.com/spf13/cobra"
)

var autoApprove bool
var prune bool

var applyCmd = &cobra.Command{
	Use:   "apply [file]",
	Short: "Apply configuration to infrastructure",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := resolveFile(args)
		eng, err := newEngine(file)
		if err != nil {
			return err
		}
		return eng.Apply(file, autoApprove, prune)
	},
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().BoolVar(&fullDiff, "full-diff", false, "Show complete diffs (no preview cutoff for new files, no size limit)")
	applyCmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	applyCmd.Flags().BoolVar(&prune, "prune", false, "Remove orphaned resources from state (does not delete from infrastructure)")
}
