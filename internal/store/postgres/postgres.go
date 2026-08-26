package postgres

import (
	"context"
	"errors"
	"time"

	"developa/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrInvalidConfig = errors.New("invalid database configuration")
	ErrUnavailable   = errors.New("database unavailable")
)

type Config struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	ConnectTimeout  time.Duration
	AnalysisEnabled bool
}

type Store struct {
	pool            *pgxpool.Pool
	analysisEnabled bool
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	ctx, span := otel.Tracer("denverr/postgres").Start(ctx, "postgres.connect", databaseAttributes())
	defer span.End()
	span.AddEvent("execution.started")
	poolCfg, err := poolConfig(cfg)
	if err != nil {
		telemetry.Fail(span, "invalid_database_configuration")
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	store, err := openPool(ctx, poolCfg)
	if err != nil {
		telemetry.Fail(span, "database_unavailable")
		return nil, err
	}
	store.analysisEnabled = cfg.AnalysisEnabled
	span.AddEvent("execution.completed")
	return store, nil
}

func poolConfig(cfg Config) (*pgxpool.Config, error) {
	if cfg.URL == "" || cfg.ConnectTimeout <= 0 {
		return nil, ErrInvalidConfig
	}
	if cfg.MaxConns < 1 || cfg.MinConns < 0 || cfg.MinConns > cfg.MaxConns {
		return nil, ErrInvalidConfig
	}
	parsed, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// pgx errors may embed the original connection string, including its password.
		return nil, ErrInvalidConfig
	}
	parsed.MaxConns = cfg.MaxConns
	parsed.MinConns = cfg.MinConns
	parsed.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	parsed.MaxConnIdleTime = 5 * time.Minute
	parsed.MaxConnLifetime = time.Hour
	return parsed, nil
}

func openPool(ctx context.Context, cfg *pgxpool.Config) (*Store, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, ErrUnavailable
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, ErrUnavailable
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, span := otel.Tracer("denverr/postgres").Start(ctx, "postgres.ping", databaseAttributes())
	defer span.End()
	span.AddEvent("execution.started")
	if err := s.pool.Ping(ctx); err != nil {
		telemetry.Fail(span, "database_unavailable")
		return ErrUnavailable
	}
	span.AddEvent("execution.completed")
	return nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func databaseAttributes() trace.SpanStartOption {
	return trace.WithAttributes(attribute.String("db.system.name", "postgresql"))
}
