package cmd

import (
	"fmt"
	"os"

	secretslib "github.com/anbraten/bull/internal/secrets"
	"github.com/spf13/cobra"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage encrypted secrets",
}

var secretsEditCmd = &cobra.Command{
	Use:   "edit [file]",
	Short: "Edit encrypted secrets for a config",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configFile := resolveFile(args)
		path, err := secretslib.ResolvePath(configFile, secretsFile)
		if err != nil {
			return err
		}

		key := secretKey
		if key == "" {
			key = os.Getenv("BULL_SECRET_KEY")
		}
		if key == "" {
			return fmt.Errorf("missing secrets key: set --secret-key or BULL_SECRET_KEY")
		}

		if err := secretslib.Edit(path, key); err != nil {
			return err
		}
		fmt.Printf("Updated encrypted secrets: %s\n", path)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(secretsCmd)
	secretsCmd.AddCommand(secretsEditCmd)
}
