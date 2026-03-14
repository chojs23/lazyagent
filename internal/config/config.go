package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	DataDir         string
	DBPath          string
	RefreshInterval time.Duration
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home: %w", err)
	}

	dataDir := os.Getenv("LAZYAGENT_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(home, ".lazyagent")
	}

	dbPath := os.Getenv("LAZYAGENT_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "observe.db")
	} else {
		dataDir = filepath.Dir(dbPath)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	return Config{
		DataDir:         dataDir,
		DBPath:          dbPath,
		RefreshInterval: time.Second,
	}, nil
}
