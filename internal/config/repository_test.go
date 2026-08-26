package config

import (
	"strings"
	"testing"
	"time"
)

func TestRepositoryConfigurationDefaults(t *testing.T) {
	cfg, err := load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.WatchInterval != 2*time.Second || cfg.ScanTimeout != 30*time.Second {
		t.Fatalf("unsafe or unexpected defaults: %+v", cfg)
	}
}

func TestRepositoryConfigurationOverrides(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL": "postgres://localhost/db", "REPOSITORY_PATH": "/workspace/repo",
		"REPOSITORY_NAME": "Example", "DEVELOPA_API_TOKEN": strings.Repeat("x", 24),
		"WATCH_INTERVAL": "250ms", "SCAN_TIMEOUT": "5m",
	}
	cfg, err := load(environment(values))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RepositoryPath != "/workspace/repo" || cfg.RepositoryName != "Example" || cfg.APIKey != values["DEVELOPA_API_TOKEN"] {
		t.Fatal("repository settings not loaded")
	}
	if cfg.WatchInterval != 250*time.Millisecond || cfg.ScanTimeout != 5*time.Minute {
		t.Fatal("repository timing overrides not applied")
	}
}

func TestRepositoryConfigurationRejectsUnsafeSettings(t *testing.T) {
	cases := []struct{ key, value string }{
		{"DEVELOPA_API_TOKEN", ""}, {"DEVELOPA_API_TOKEN", "short-secret"},
		{"WATCH_INTERVAL", "249ms"}, {"WATCH_INTERVAL", "61s"}, {"WATCH_INTERVAL", "bad"},
		{"SCAN_TIMEOUT", "0s"}, {"SCAN_TIMEOUT", "6m"}, {"REPOSITORY_PATH", "/repo\x00"},
	}
	for _, tc := range cases {
		values := map[string]string{
			"DATABASE_URL": "postgres://localhost/db", "REPOSITORY_PATH": "/workspace/repo",
			"DEVELOPA_API_TOKEN": strings.Repeat("x", 24),
		}
		values[tc.key] = tc.value
		_, err := load(environment(values))
		if err == nil {
			t.Fatalf("unsafe setting accepted: %s", tc.key)
		}
		if strings.Contains(err.Error(), "short-secret") {
			t.Fatal("configuration error leaked token")
		}
	}
}
