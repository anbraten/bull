package main

import (
	"os"

	"github.com/anbraten/bull/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
