package httptransport

import (
	"net/http"
	"net/url"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type repositoryRoutes struct {
	catalog         domain.RepositoryReader
	defaultExplorer *Explorer
	ids             []string
	handlers        map[string]http.Handler
}

func mountRepositories(router chi.Router, cfg Config) {
	if cfg.WorkspaceRuntime != nil {
		mountManagedWorkspaces(router, cfg)
		return
	}
	repositories := configuredRepositoryRoutes(cfg)
	explorer := repositories.defaultExplorer
	router.Get("/api/info", explorer.info)
	router.Route("/api", func(api chi.Router) {
		// The operator configures one root credential. Selected repositories do
		// not get to replace authentication or choose their own token.
		api.Use(explorer.authenticate)
		api.Get("/repositories", repositories.list)
		api.Mount("/repositories/{repository}", http.HandlerFunc(repositories.scoped))
		api.Group(func(defaultAPI chi.Router) {
			defaultAPI.Use(repositoryTrace(explorer))
			explorer.mountRoutes(defaultAPI)
		})
	})
}

func configuredRepositoryRoutes(cfg Config) repositoryRoutes {
	explorers := cfg.Explorers
	if len(explorers) == 0 {
		explorers = []*Explorer{cfg.Explorer}
	}
	routes := repositoryRoutes{catalog: cfg.RepositoryCatalog, defaultExplorer: explorers[0], handlers: map[string]http.Handler{}}
	for _, explorer := range explorers {
		if !explorer.configured() || routes.handlers[explorer.RepositoryID] != nil {
			continue
		}
		scoped := chi.NewRouter()
		scoped.Use(repositoryTrace(explorer))
		explorer.mountRoutes(scoped)
		routes.handlers[explorer.RepositoryID] = scoped
		routes.ids = append(routes.ids, explorer.RepositoryID)
	}
	return routes
}

func (routes repositoryRoutes) scoped(w http.ResponseWriter, r *http.Request) {
	handler := routes.handlers[chi.URLParam(r, "repository")]
	if handler == nil {
		writeStatus(w, http.StatusNotFound, "not_found")
		return
	}
	handler.ServeHTTP(w, r)
}

func repositoryTrace(explorer *Explorer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if explorer.configured() {
				trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("repository.id", explorer.RepositoryID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (routes repositoryRoutes) list(w http.ResponseWriter, r *http.Request) {
	filter, err := repositoryFilter(r.URL.RawQuery)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid_filter")
		return
	}
	if routes.catalog == nil {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	page, err := routes.catalog.Repositories(r.Context(), routes.ids, filter)
	page.DefaultRepositoryID = routes.defaultExplorer.RepositoryID
	respond(w, page, err)
}

func repositoryFilter(raw string) (domain.Filter, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return domain.Filter{}, errInvalidFilter
	}
	for key, values := range query {
		if len(values) != 1 || (key != "q" && key != "limit" && key != "offset") {
			return domain.Filter{}, errInvalidFilter
		}
	}
	return parseFilter(query, 24)
}
