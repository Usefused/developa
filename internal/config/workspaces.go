package config

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

func loadWorkspaceRoots(lookup lookupEnv, cfg *Config) error {
	raw := value(lookup, "WORKSPACE_ROOTS", "")
	if raw == "" {
		return nil
	}
	if len(raw) > 65536 {
		return errors.New("WORKSPACE_ROOTS is too large")
	}
	if err := json.Unmarshal([]byte(raw), &cfg.WorkspaceRoots); err != nil {
		return errors.New("WORKSPACE_ROOTS must be a JSON array of absolute folder paths")
	}
	if cfg.WorkspaceRoots == nil {
		return errors.New("WORKSPACE_ROOTS must be a JSON array")
	}
	if len(cfg.WorkspaceRoots) > 16 {
		return errors.New("WORKSPACE_ROOTS supports at most 16 folders")
	}
	for _, root := range cfg.WorkspaceRoots {
		if !filepath.IsAbs(root) || len(root) > 4096 || strings.ContainsRune(root, 0) {
			return errors.New("WORKSPACE_ROOTS requires absolute folder paths of at most 4096 bytes")
		}
	}
	return nil
}
