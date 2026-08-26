package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains operator-owned settings, never repository-provided options.
type Config struct {
	HTTPAddr               string
	DatabaseURL            string
	DatabaseMaxConns       int32
	DatabaseMinConns       int32
	DatabaseConnectTimeout time.Duration
	ReadinessTimeout       time.Duration
	RequestTimeout         time.Duration
	ShutdownTimeout        time.Duration
	ServiceName            string
	TelemetryEndpoint      string
	RepositoryPath         string
	RepositoryName         string
	Repositories           []Repository
	WorkspaceRoots         []string
	APIKey                 string `json:"-"`
	WatchInterval          time.Duration
	ScanTimeout            time.Duration
	OllamaURL              string
	OllamaModel            string
	OllamaAnalysisModel    string
	OllamaAnswerModel      string
	OllamaCloud            bool
	OllamaAPIKey           string `json:"-"`
	OllamaTimeout          time.Duration
	AITimeout              time.Duration
	AIIndexEnabled         bool
	AIAutoFeatures         bool
	AIPollInterval         time.Duration
}

type lookupEnv func(string) (string, bool)

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup lookupEnv) (Config, error) {
	cfg := Config{
		HTTPAddr:          value(lookup, "HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:       value(lookup, "DATABASE_URL", ""),
		ServiceName:       value(lookup, "OTEL_SERVICE_NAME", "developa"),
		TelemetryEndpoint: value(lookup, "OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		RepositoryPath:    value(lookup, "REPOSITORY_PATH", ""),
		RepositoryName:    value(lookup, "REPOSITORY_NAME", ""),
		APIKey:            value(lookup, "DEVELOPA_API_TOKEN", ""),
	}
	if err := loadTimeouts(lookup, &cfg); err != nil {
		return Config{}, err
	}
	if err := loadPool(lookup, &cfg); err != nil {
		return Config{}, err
	}
	if err := loadRepository(lookup, &cfg); err != nil {
		return Config{}, err
	}
	if err := loadIntelligence(lookup, &cfg); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadTimeouts(lookup lookupEnv, cfg *Config) error {
	var errs [4]error
	cfg.DatabaseConnectTimeout, errs[0] = duration(lookup, "DATABASE_CONNECT_TIMEOUT", 5*time.Second)
	cfg.ReadinessTimeout, errs[1] = duration(lookup, "READINESS_TIMEOUT", 2*time.Second)
	cfg.RequestTimeout, errs[2] = duration(lookup, "REQUEST_TIMEOUT", 10*time.Second)
	cfg.ShutdownTimeout, errs[3] = duration(lookup, "SHUTDOWN_TIMEOUT", 10*time.Second)
	return errors.Join(errs[:]...)
}

func loadPool(lookup lookupEnv, cfg *Config) error {
	var maxErr, minErr error
	cfg.DatabaseMaxConns, maxErr = poolSize(lookup, "DATABASE_MAX_CONNS", 10)
	cfg.DatabaseMinConns, minErr = poolSize(lookup, "DATABASE_MIN_CONNS", 0)
	return errors.Join(maxErr, minErr)
}

func validate(cfg Config) error {
	if err := validateAddress(cfg.HTTPAddr); err != nil {
		return err
	}
	if err := validateDatabaseURL(cfg.DatabaseURL); err != nil {
		return err
	}
	if cfg.DatabaseMaxConns == 0 || cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return errors.New("database pool sizes require 0 <= min <= max and max > 0")
	}
	if cfg.ReadinessTimeout > cfg.RequestTimeout {
		return errors.New("READINESS_TIMEOUT must not exceed REQUEST_TIMEOUT")
	}
	return validateRepository(cfg)
}

func validateAddress(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("HTTP_ADDR must contain a host and port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("HTTP_ADDR must have a valid numeric port")
	}
	return nil
}

func validateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("DATABASE_URL must be a valid PostgreSQL URL")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return errors.New("DATABASE_URL must use postgres or postgresql")
	}
	if u.Hostname() == "" || u.Fragment != "" {
		return errors.New("DATABASE_URL requires a host and must not have a fragment")
	}
	return nil
}

func value(lookup lookupEnv, name, fallback string) string {
	v, ok := lookup(name)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func duration(lookup lookupEnv, name string, fallback time.Duration) (time.Duration, error) {
	v, err := time.ParseDuration(value(lookup, name, fallback.String()))
	if err != nil || v <= 0 || v > 5*time.Minute {
		return 0, fmt.Errorf("%s must be a positive duration no greater than 5m", name)
	}
	return v, nil
}

func poolSize(lookup lookupEnv, name string, fallback int32) (int32, error) {
	v, err := strconv.ParseInt(value(lookup, name, strconv.Itoa(int(fallback))), 10, 32)
	if err != nil || v < 0 || v > 1000 {
		return 0, fmt.Errorf("%s must be an integer from 0 through 1000", name)
	}
	return int32(v), nil
}
