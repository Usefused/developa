package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

type ReadinessChecker interface {
	Ping(context.Context) error
}

type Config struct {
	Address             string
	ReadinessTimeout    time.Duration
	RequestTimeout      time.Duration
	AITimeout           time.Duration
	Explorer            *Explorer
	Explorers           []*Explorer
	RepositoryCatalog   domain.RepositoryReader
	WorkspaceManagement domain.WorkspaceManagement
	WorkspaceRuntime    WorkspaceRuntime
	UI                  http.Handler
}

func NewServer(checker ReadinessChecker, cfg Config) *http.Server {
	return &http.Server{
		Addr:              cfg.Address,
		Handler:           NewHandler(checker, cfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.RequestTimeout,
		// Failed executions get five seconds to finalize durable audit records before the response.
		WriteTimeout:   max(cfg.RequestTimeout, cfg.AITimeout) + 6*time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
}

func NewHandler(checker ReadinessChecker, cfg Config) http.Handler {
	router := chi.NewRouter()
	router.Use(traceRequest, recoverRequest, executionTimeout(cfg))
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})
	router.Get("/readyz", readinessHandler(checker, cfg.ReadinessTimeout))
	router.Get("/api/openapi.json", serveOpenAPI)
	mountRepositories(router, cfg)
	mountUI(router, cfg.UI)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusNotFound, "not_found")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusMethodNotAllowed, "method_not_allowed")
	})
	return router
}

func readinessHandler(checker ReadinessChecker, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := checker.Ping(ctx); err != nil {
			writeStatus(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeStatus(w, http.StatusOK, "ready")
	}
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	writeJSON(w, code, StatusResponse{Status: status})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func mountUI(router chi.Router, handler http.Handler) {
	if handler == nil {
		return
	}
	for _, path := range []string{"/", "/blocks", "/flow", "/changes", "/analysis", "/features", "/chain"} {
		router.Method(http.MethodGet, path, handler)
	}
	router.Method(http.MethodGet, "/assets/*", handler)
}
