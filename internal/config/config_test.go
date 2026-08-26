package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	cfg, err := load(environment(map[string]string{
		"DATABASE_URL": "postgres://user:secret@localhost/developa",
		"HTTP_ADDR":    "127.0.0.1:9090", "DATABASE_MAX_CONNS": "24",
		"DATABASE_MIN_CONNS": "2", "REQUEST_TIMEOUT": "30s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" || cfg.DatabaseMaxConns != 24 || cfg.DatabaseMinConns != 2 {
		t.Fatalf("operator overrides not applied")
	}
	if cfg.ReadinessTimeout != 2*time.Second || cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("unexpected timeouts")
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	cases := []struct{ name, key, value string }{
		{"missing database", "DATABASE_URL", ""},
		{"wrong database scheme", "DATABASE_URL", "https://localhost/db"},
		{"no host", "DATABASE_URL", "postgres:///db"},
		{"invalid address", "HTTP_ADDR", "localhost"},
		{"invalid port", "HTTP_ADDR", ":70000"},
		{"zero pool", "DATABASE_MAX_CONNS", "0"},
		{"negative pool", "DATABASE_MIN_CONNS", "-1"},
		{"inverted pool", "DATABASE_MIN_CONNS", "11"},
		{"large pool", "DATABASE_MAX_CONNS", "1001"},
		{"invalid duration", "READINESS_TIMEOUT", "not-a-duration"},
		{"zero duration", "SHUTDOWN_TIMEOUT", "0s"},
		{"long duration", "REQUEST_TIMEOUT", "6m"},
		{"inverted timeout", "READINESS_TIMEOUT", "11s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"DATABASE_URL": "postgres://localhost/db", tc.key: tc.value}
			if _, err := load(environment(env)); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
}

func TestLoadDoesNotEchoSecrets(t *testing.T) {
	secret := "password-secret-never-log"
	_, err := load(environment(map[string]string{"DATABASE_URL": "postgres://user:" + secret + "@%invalid"}))
	if err == nil {
		t.Fatal("expected malformed URL to fail")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("configuration error disclosed a secret")
	}
}

func TestLoadUsesProcessEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/process")
	t.Setenv("HTTP_ADDR", ":9091")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9091" {
		t.Fatal("Load did not read process environment")
	}
}

func environment(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
