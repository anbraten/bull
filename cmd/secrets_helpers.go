package cmd

import (
	"os"

	"github.com/anbraten/bull/internal/engine"
	secretslib "github.com/anbraten/bull/internal/secrets"
)

func newEngine(configFile string) (*engine.Engine, error) {
	secretsPath, err := secretslib.ResolvePath(configFile, secretsFile)
	if err != nil {
		return nil, err
	}

	key := secretKey
	if key == "" {
		key = os.Getenv("BULL_SECRET_KEY")
	}

	values, err := secretslib.Load(secretsPath, key)
	if err != nil {
		return nil, err
	}

	return engine.New(verbose, values), nil
}
