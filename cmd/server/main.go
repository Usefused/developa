package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"developa/internal/application"
	"developa/internal/config"
	"developa/internal/store/postgres"
	"developa/internal/telemetry"
	"go.opentelemetry.io/otel"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{ServiceName: cfg.ServiceName, Endpoint: cfg.TelemetryEndpoint})
	if err != nil {
		return err
	}
	defer flushTelemetry(shutdownTelemetry)
	store, err := connectDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	workspaces, err := startExplorer(ctx, store, cfg)
	if err != nil {
		return err
	}
	defer workspaces.Close()
	server, stopWorkers, err := managedExplorerServer(ctx, store, workspaces, cfg)
	if err != nil {
		return err
	}
	defer stopWorkers()
	return serve(ctx, server, cfg.ShutdownTimeout)
}

func startExplorer(ctx context.Context, store *postgres.Store, cfg config.Config) (*application.Workspaces, error) {
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := store.Migrate(startup); err != nil {
		return nil, errors.New("catalog migration failed")
	}
	workspaces, err := application.NewPersistentWorkspaces(startup, store, repositoryManagers(cfg), managerDefaults(cfg))
	if err != nil {
		return nil, err
	}
	if len(workspaces.Managers()) > 0 && len(cfg.APIKey) < 24 {
		workspaces.Close()
		return nil, errors.New("DEVELOPA_API_TOKEN must contain at least 24 bytes for saved workspaces")
	}
	workspaces.Start(ctx)
	return workspaces, nil
}

func connectDatabase(ctx context.Context, cfg config.Config) (*postgres.Store, error) {
	ctx, span := otel.Tracer("developa/server").Start(ctx, "server.startup")
	defer span.End()
	span.AddEvent("execution.started")
	store, err := postgres.Open(ctx, postgres.Config{
		URL: cfg.DatabaseURL, MaxConns: cfg.DatabaseMaxConns,
		MinConns: cfg.DatabaseMinConns, ConnectTimeout: cfg.DatabaseConnectTimeout,
		AnalysisEnabled: cfg.AIIndexEnabled && cfg.AIAutoFeatures && cfg.OllamaAnalysisModel != "",
	})
	if err != nil {
		telemetry.Fail(span, "database_unavailable")
		return nil, err
	}
	span.AddEvent("execution.completed")
	return store, nil
}

func serve(ctx context.Context, server *http.Server, timeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	slog.Info("HTTP server starting", "address", server.Addr)
	select {
	case err := <-errCh:
		return serverError(err)
	case <-ctx.Done():
		return shutdownServer(server, timeout)
	}
}

func shutdownServer(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		return errors.New("HTTP server graceful shutdown timed out")
	}
	slog.Info("HTTP server stopped")
	return nil
}

func serverError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.New("HTTP server could not listen or serve")
}

func flushTelemetry(shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "OpenTelemetry shutdown failed")
	}
}
