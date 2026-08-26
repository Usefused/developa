package config

import (
	"errors"
	"strings"
	"time"
)

func loadRepository(lookup lookupEnv, cfg *Config) error {
	var watchErr, scanErr error
	cfg.WatchInterval, watchErr = duration(lookup, "WATCH_INTERVAL", 2*time.Second)
	cfg.ScanTimeout, scanErr = duration(lookup, "SCAN_TIMEOUT", 30*time.Second)
	return errors.Join(watchErr, scanErr, loadRepositories(lookup, cfg), loadWorkspaceRoots(lookup, cfg))
}

func validateRepository(cfg Config) error {
	if cfg.WatchInterval < 250*time.Millisecond || cfg.WatchInterval > time.Minute {
		return errors.New("WATCH_INTERVAL must be from 250ms through 1m")
	}
	if (len(cfg.Repositories) > 0 || len(cfg.WorkspaceRoots) > 0) && len(cfg.APIKey) < 24 {
		return errors.New("DEVELOPA_API_TOKEN must contain at least 24 bytes when repositories are configured")
	}
	if strings.ContainsRune(cfg.RepositoryPath, 0) || strings.ContainsRune(cfg.APIKey, 0) {
		return errors.New("repository settings must not contain NUL characters")
	}
	return nil
}
