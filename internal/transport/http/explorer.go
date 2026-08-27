package httptransport

import (
	"context"
	"errors"
	"net/http"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

// Explorer contains operator-configured services and repository scope. Requests
// cannot select arbitrary local directories or expand beyond this repository.
type Explorer struct {
	Catalog             domain.CatalogReader
	Tracker             domain.Tracker
	RepositoryID        string
	Token               string
	Knowledge           domain.IntelligenceStore
	FeatureContexts     domain.FeatureContextService
	Intelligence        domain.Intelligence
	Reviewer            domain.FunctionReviewer
	Jobs                domain.AnalysisQueue
	OllamaCloud         bool
	AutomaticFeatures   bool
	WorkspaceManagement bool
}

func (e *Explorer) mountRoutes(api chi.Router) {
	api.Get("/project", e.project)
	api.Post("/scan", e.scan)
	api.Get("/capabilities", e.capabilities)
	api.Route("/snapshots/{snapshot}", func(snapshot chi.Router) {
		snapshot.Use(validateSnapshot)
		snapshot.Get("/files", e.files)
		snapshot.Get("/file", e.file)
		snapshot.Get("/symbols", e.symbols)
		snapshot.Get("/symbols/{symbol}", e.symbol)
		snapshot.Get("/symbols/{symbol}/source", e.symbolSource)
		snapshot.Get("/symbols/{symbol}/implementations", e.implementations)
		snapshot.Get("/details", e.details)
		e.mountKnowledge(snapshot)
	})
}

func (e *Explorer) configured() bool {
	return e != nil && e.RepositoryID != "" && e.Catalog != nil && e.Tracker != nil
}

func (e *Explorer) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, InfoResponse{AuthenticationRequired: e.configured() || e.managementEnabled(), Configured: e.configured(), WorkspaceManagement: e.managementEnabled()})
}

func (e *Explorer) managementEnabled() bool { return e != nil && e.WorkspaceManagement }

func (e *Explorer) project(w http.ResponseWriter, r *http.Request) {
	project, err := e.Tracker.Project(r.Context())
	respond(w, project, err)
}

func (e *Explorer) files(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r.URL.Query(), 24)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	page, err := e.Catalog.Files(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), filter)
	respond(w, page, err)
}

func (e *Explorer) file(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("path")
	if !validPath(file) {
		writeStatus(w, http.StatusBadRequest, "invalid_path")
		return
	}
	detail, err := e.Catalog.File(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), file)
	respond(w, detail, err)
}

func (e *Explorer) symbols(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r.URL.Query(), 50)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	page, err := e.Catalog.Symbols(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), filter)
	respond(w, page, err)
}

func (e *Explorer) symbol(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if !validID(symbol) {
		writeStatus(w, http.StatusBadRequest, "invalid_symbol")
		return
	}
	detail, err := e.Catalog.Symbol(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"), symbol)
	respond(w, detail, err)
}

func (e *Explorer) details(w http.ResponseWriter, r *http.Request) {
	detail, err := e.Catalog.Details(r.Context(), e.RepositoryID, chi.URLParam(r, "snapshot"))
	respond(w, detail, err)
}

func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func writeError(w http.ResponseWriter, err error) {
	code, status := errorStatus(err)
	writeStatus(w, code, status)
}

func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, domain.ErrBusy):
		return http.StatusConflict, "busy"
	case errors.Is(err, domain.ErrSourceUnavailable):
		return http.StatusConflict, "source_unavailable"
	case errors.Is(err, domain.ErrNotConfigured):
		return http.StatusServiceUnavailable, "not_configured"
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, domain.ErrModelUnavailable):
		return http.StatusServiceUnavailable, "model_unavailable"
	case errors.Is(err, domain.ErrInvalidModelOutput):
		return http.StatusBadGateway, "invalid_model_output"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "execution_timed_out"
	default:
		// Underlying errors can include SQL, source paths, or connection secrets.
		return http.StatusServiceUnavailable, "unavailable"
	}
}
