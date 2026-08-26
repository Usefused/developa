package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func loadRepository(lookup lookupEnv, cfg *Config) error {
	var watchErr, scanErr, fileErr, totalErr error
	cfg.WatchInterval, watchErr = duration(lookup, "WATCH_INTERVAL", 2*time.Second)
	cfg.ScanTimeout, scanErr = duration(lookup, "SCAN_TIMEOUT", 30*time.Second)
	cfg.SourceMaxFileBytes, fileErr = sourceBytes(lookup, "SOURCE_MAX_FILE_BYTES", 4<<20)
	cfg.SourceMaxTotalBytes, totalErr = sourceBytes(lookup, "SOURCE_MAX_TOTAL_BYTES", 64<<20)
	return errors.Join(watchErr, scanErr, fileErr, totalErr, loadRepositories(lookup, cfg), loadWorkspaceRoots(lookup, cfg))
}

func validateRepository(cfg Config) error {
	if cfg.WatchInterval < 250*time.Millisecond || cfg.WatchInterval > time.Minute {
		return errors.New("WATCH_INTERVAL must be from 250ms through 1m")
	}
	if (len(cfg.Repositories) > 0 || len(cfg.WorkspaceRoots) > 0) && len(cfg.APIKey) < 24 {
		return errors.New("DENVERR_API_TOKEN must contain at least 24 bytes when repositories are configured")
	}
	if strings.ContainsRune(cfg.RepositoryPath, 0) || strings.ContainsRune(cfg.APIKey, 0) {
		return errors.New("repository settings must not contain NUL characters")
	}
	if cfg.SourceMaxFileBytes > cfg.SourceMaxTotalBytes {
		return errors.New("SOURCE_MAX_TOTAL_BYTES must cover at least one maximum-size source file")
	}
	return nil
}

func sourceBytes(lookup lookupEnv, name string, fallback int64) (int64, error) {
	value := value(lookup, name, strconv.FormatInt(fallback, 10))
	bytes, err := strconv.ParseInt(value, 10, 64)
	if err != nil || bytes <= 0 || bytes > 1<<30 {
		return 0, fmt.Errorf("%s must be an integer byte count from 1 through 1073741824", name)
	}
	return bytes, nil
}
