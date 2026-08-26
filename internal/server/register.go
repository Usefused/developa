package server

import (
	"context"
	"strings"

	"developa/internal/application"
	"developa/internal/config"
	"developa/internal/domain"
	"developa/internal/telemetry"
	"go.opentelemetry.io/otel"
)

// RegisterWorkspace persists a user-selected Git checkout without requiring the HTTP server to be running.
func RegisterWorkspace(ctx context.Context, cfg config.Config, path, name string) (result domain.AddedWorkspace, err error) {
	ctx, span := otel.Tracer("denverr/server").Start(ctx, "workspace.add")
	defer span.End()
	span.AddEvent("execution.started")
	defer func() {
		if err != nil {
			telemetry.Fail(span, "workspace_registration_failed")
		} else {
			span.AddEvent("execution.completed")
		}
	}()
	store, err := connectDatabase(ctx, cfg)
	if err != nil {
		return result, err
	}
	defer store.Close()
	startup, cancel, err := migrateCatalog(ctx, store)
	if err != nil {
		return result, err
	}
	defer cancel()
	group, err := application.NewPersistentWorkspaces(startup, store, nil, managerDefaults(cfg))
	if err != nil {
		return result, err
	}
	defer group.Close()
	manager, reused, err := group.Add(startup, application.ManagerConfig{
		RepositoryPath: path, RepositoryName: strings.TrimSpace(name),
		PollInterval: cfg.WatchInterval, ScanTimeout: cfg.ScanTimeout,
		MaxFileBytes: cfg.SourceMaxFileBytes, MaxTotalBytes: cfg.SourceMaxTotalBytes,
	})
	if err != nil {
		return result, err
	}
	return domain.AddedWorkspace{Repository: manager.Repository(), AlreadyAdded: reused}, nil
}
