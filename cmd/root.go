package cmd

import (
	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:          "bull",
	Short:        "Stupid simple infrastructure management",
	SilenceUsage: true, // don't dump usage on every runtime error
	Long: `Bull manages infrastructure using Lua configuration files.

Define hosts, services, DNS records and more in plain Lua.
Use components (plain Lua functions) to group related resources.

Example:
  bull plan infra.lua     # preview changes
  bull apply infra.lua    # apply changes`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
}
