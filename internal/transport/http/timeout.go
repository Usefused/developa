package httptransport

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func executionTimeout(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		ordinary := middleware.Timeout(cfg.RequestTimeout)(next)
		model := middleware.Timeout(max(cfg.RequestTimeout, cfg.AITimeout))(next)
		jobs := streamContextTimeout(eventStreamLifetime)(next)
		answers := streamContextTimeout(max(cfg.RequestTimeout, cfg.AITimeout))(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAnalysisStream(r) {
				jobs.ServeHTTP(w, r)
				return
			}
			if isAnswerStream(r) {
				answers.ServeHTTP(w, r)
				return
			}
			if isModelRequest(r) {
				model.ServeHTTP(w, r)
				return
			}
			ordinary.ServeHTTP(w, r)
		})
	}
}

func streamContextTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			// SSE handlers own terminal events; an HTTP status cannot replace headers
			// already sent when a stream deadline ends.
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isModelRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	endpoint, ok := snapshotEndpoint(r)
	return ok && (endpoint == "answers" || endpoint == "answers/stream" || endpoint == "function-reviews" || endpoint == "function-reviews/stream")
}

func isAnalysisStream(r *http.Request) bool {
	endpoint, ok := snapshotEndpoint(r)
	return r.Method == http.MethodGet && ok && endpoint == "events"
}

func isAnswerStream(r *http.Request) bool {
	endpoint, ok := snapshotEndpoint(r)
	return r.Method == http.MethodPost && ok && (endpoint == "answers/stream" || endpoint == "function-reviews/stream")
}

func snapshotEndpoint(r *http.Request) (string, bool) {
	path, prefix := strings.CutPrefix(snapshotRoutePath(r.URL.Path), "/api/snapshots/")
	id, endpoint, found := strings.Cut(path, "/")
	return endpoint, prefix && found && validID(id)
}

func snapshotRoutePath(path string) string {
	scoped, found := strings.CutPrefix(path, "/api/repositories/")
	if !found {
		return path
	}
	id, endpoint, found := strings.Cut(scoped, "/")
	if !found || !validID(id) {
		return ""
	}
	return "/api/" + endpoint
}
