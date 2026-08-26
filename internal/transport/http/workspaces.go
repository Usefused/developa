package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

type WorkspaceRuntime interface {
	Explorer(string) (*Explorer, error)
	RepositoryIDs() []string
}

type managedWorkspaces struct{ config Config }

func mountManagedWorkspaces(router chi.Router, cfg Config) {
	routes := managedWorkspaces{config: cfg}
	router.Get("/api/info", func(w http.ResponseWriter, r *http.Request) {
		explorer, err := cfg.WorkspaceRuntime.Explorer("")
		if err != nil {
			writeError(w, err)
			return
		}
		explorer.info(w, r)
	})
	router.Route("/api", func(api chi.Router) {
		api.Use(cfg.Explorer.authenticate)
		api.Get("/repositories", routes.list)
		api.Post("/repositories", routes.add)
		api.Post("/repositories/resolve", routes.resolve)
		api.Get("/workspace-roots", routes.roots)
		api.Get("/workspace-folders", routes.folders)
		api.Mount("/repositories/{repository}", http.HandlerFunc(routes.scoped))
		api.Mount("/", http.HandlerFunc(routes.scoped))
	})
}

func (m managedWorkspaces) resolve(w http.ResponseWriter, r *http.Request) {
	request, err := repositoryPathBody(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := m.config.WorkspaceManagement.ResolveRepository(r.Context(), request)
	respond(w, result, err)
}

func (m managedWorkspaces) scoped(w http.ResponseWriter, r *http.Request) {
	explorer, err := m.config.WorkspaceRuntime.Explorer(chi.URLParam(r, "repository"))
	if err != nil {
		writeError(w, err)
		return
	}
	router := chi.NewRouter()
	router.Use(repositoryTrace(explorer))
	explorer.mountRoutes(router)
	router.ServeHTTP(w, r)
}

func (m managedWorkspaces) list(w http.ResponseWriter, r *http.Request) {
	explorer, err := m.config.WorkspaceRuntime.Explorer("")
	if err != nil {
		writeError(w, err)
		return
	}
	routes := repositoryRoutes{catalog: m.config.RepositoryCatalog, defaultExplorer: explorer, ids: m.config.WorkspaceRuntime.RepositoryIDs()}
	routes.list(w, r)
}

func (m managedWorkspaces) roots(w http.ResponseWriter, r *http.Request) {
	roots, err := m.config.WorkspaceManagement.FolderRoots(r.Context())
	respond(w, roots, err)
}

func (m managedWorkspaces) folders(w http.ResponseWriter, r *http.Request) {
	query, offset, err := folderQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := m.config.WorkspaceManagement.Folders(r.Context(), query.Get("root_id"), query.Get("path"), offset)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	respond(w, page, nil)
}

func folderQuery(raw string) (url.Values, int, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return nil, 0, domain.ErrInvalidInput
	}
	for key, values := range query {
		if len(values) != 1 || (key != "root_id" && key != "path" && key != "offset") {
			return nil, 0, domain.ErrInvalidInput
		}
	}
	offset := 0
	if query.Get("offset") != "" {
		offset, err = strconv.Atoi(query.Get("offset"))
	}
	if err != nil {
		return nil, 0, domain.ErrInvalidInput
	}
	return query, offset, nil
}

func (m managedWorkspaces) add(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeStatus(w, http.StatusForbidden, "cross_origin_forbidden")
		return
	}
	request, err := workspaceBody(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := m.config.WorkspaceManagement.AddWorkspace(r.Context(), request)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	// Initializing the repository services does not make an inference request.
	if _, err := m.config.WorkspaceRuntime.Explorer(result.ID); err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if result.AlreadyAdded {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func workspaceBody(w http.ResponseWriter, r *http.Request) (domain.AddWorkspaceRequest, error) {
	var request domain.AddWorkspaceRequest
	if err := strictWorkspaceJSON(w, r, &request); err != nil {
		return request, err
	}
	if request.RootID == "" || request.Path == "" {
		return request, domain.ErrInvalidInput
	}
	return request, nil
}

func repositoryPathBody(w http.ResponseWriter, r *http.Request) (domain.ResolveRepositoryRequest, error) {
	var request domain.ResolveRepositoryRequest
	if err := strictWorkspaceJSON(w, r, &request); err != nil {
		return request, err
	}
	if request.Path == "" {
		return request, domain.ErrInvalidInput
	}
	return request, nil
}

func strictWorkspaceJSON(w http.ResponseWriter, r *http.Request, value any) error {
	if !jsonContentType(r.Header.Get("Content-Type")) {
		return domain.ErrInvalidInput
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return domain.ErrInvalidInput
	}
	if !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return domain.ErrInvalidInput
	}
	return nil
}

func writeWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotGitRepository):
		writeStatus(w, http.StatusUnprocessableEntity, "not_git_repository")
	case errors.Is(err, domain.ErrFolderForbidden):
		writeStatus(w, http.StatusForbidden, "folder_forbidden")
	case errors.Is(err, domain.ErrWorkspaceLimit):
		writeStatus(w, http.StatusConflict, "workspace_limit")
	default:
		writeError(w, err)
	}
}
