package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOpenRejectsInvalidConfigurationWithoutSecrets(t *testing.T) {
	secret := "secret-database-password"
	cfg := Config{URL: "postgres://user:" + secret + "@%invalid", MaxConns: 2, ConnectTimeout: time.Second}
	_, err := Open(context.Background(), cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid configuration, got %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("database error disclosed the password")
	}
}

func TestPoolConfigRejectsInvalidBounds(t *testing.T) {
	cases := []Config{
		{URL: "postgres://localhost/db", MaxConns: 1},
		{URL: "postgres://localhost/db", ConnectTimeout: time.Second},
		{URL: "postgres://localhost/db", MaxConns: 1, MinConns: 2, ConnectTimeout: time.Second},
		{URL: "postgres://localhost/db", MaxConns: 1, MinConns: -1, ConnectTimeout: time.Second},
	}
	for _, cfg := range cases {
		if _, err := poolConfig(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected invalid bounds, got %v", err)
		}
	}
}

func TestIntegrationOpenPingAndClose(t *testing.T) {
	cfg := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := store.Ping(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable after close, got %v", err)
	}
}

func TestIntegrationCanceledDatabaseConnect(t *testing.T) {
	cfg := integrationConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store, err := Open(ctx, cfg)
	if store != nil {
		store.Close()
		t.Fatal("canceled startup must not return a usable store")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected canceled connect to fail safely, got %v", err)
	}
}

func TestIntegrationDatabaseTraceParentAndRedaction(t *testing.T) {
	cfg := integrationConfig(t)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	ctx, parent := provider.Tracer("test").Start(context.Background(), "parent")
	store, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	parent.End()
	spans := exporter.GetSpans()
	if len(spans) != 2 || spans[0].Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("database initialization did not retain trace parent")
	}
	if len(spans[0].Attributes) != 1 || spans[0].Attributes[0].Key != "db.system.name" {
		t.Fatal("database span must expose only the database system")
	}
}

func integrationConfig(t *testing.T) Config {
	t.Helper()
	connectionURL, present := os.LookupEnv("DEVELOPA_TEST_DATABASE_URL")
	if !present {
		t.Skip("set DEVELOPA_TEST_DATABASE_URL to run real PostgreSQL integration tests")
	}
	return Config{URL: connectionURL, MaxConns: 2, MinConns: 0, ConnectTimeout: 5 * time.Second}
}
