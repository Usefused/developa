package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"developa/internal/domain"
	source "developa/internal/source/git"
	"developa/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

type WorkspaceService struct {
	group    *Workspaces
	defaults ManagerConfig
	roots    []domain.FolderRoot
}

func NewWorkspaceService(group *Workspaces, defaults ManagerConfig, paths []string) (*WorkspaceService, error) {
	service := &WorkspaceService{group: group, defaults: defaults, roots: []domain.FolderRoot{}}
	for _, path := range paths {
		root, err := canonicalWorkspacePath(path)
		if err != nil {
			return nil, err
		}
		dir, err := os.OpenRoot(root)
		if err != nil {
			return nil, domain.ErrFolderForbidden
		}
		dir.Close()
		service.roots = append(service.roots, domain.FolderRoot{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(root))), Name: filepath.Base(root), Path: root})
	}
	return service, nil
}

func (s *WorkspaceService) FolderRoots(context.Context) ([]domain.FolderRoot, error) {
	return append([]domain.FolderRoot{}, s.roots...), nil
}

func (s *WorkspaceService) root(id string) (domain.FolderRoot, error) {
	for _, root := range s.roots {
		if root.ID == id {
			return root, nil
		}
	}
	return domain.FolderRoot{}, domain.ErrFolderForbidden
}

func validFolderPath(path string) bool {
	return path != "" && len(path) <= 4096 && !strings.ContainsRune(path, 0) && filepath.IsLocal(path)
}

func (s *WorkspaceService) Folders(ctx context.Context, id, path string, offset int) (domain.FolderPage, error) {
	if !validFolderPath(path) || offset < 0 || offset > 100000 {
		return domain.FolderPage{}, domain.ErrInvalidInput
	}
	root, err := s.root(id)
	if err != nil {
		return domain.FolderPage{}, err
	}
	dir, err := os.OpenRoot(root.Path)
	if err != nil {
		return domain.FolderPage{}, domain.ErrFolderForbidden
	}
	defer dir.Close()
	// Root confines every path component, including symlinks, during filesystem reads.
	folder, err := dir.Open(path)
	if err != nil {
		return domain.FolderPage{}, domain.ErrFolderForbidden
	}
	defer folder.Close()
	return readFolderPage(ctx, folder, id, path, offset)
}

func readFolderPage(ctx context.Context, folder *os.File, id, path string, offset int) (domain.FolderPage, error) {
	page := domain.FolderPage{RootID: id, Path: filepath.ToSlash(filepath.Clean(path)), Items: []domain.Folder{}}
	if err := skipFolderEntries(ctx, folder, offset); err != nil {
		return page, err
	}
	entries, err := folder.ReadDir(100)
	if err != nil && err != io.EOF {
		return page, domain.ErrFolderForbidden
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".git" {
			page.Items = append(page.Items, domain.Folder{Name: entry.Name(), Path: filepath.ToSlash(filepath.Join(path, entry.Name()))})
		}
	}
	if len(entries) == 100 {
		next := offset + 100
		page.NextOffset = &next
	}
	return page, ctx.Err()
}

func skipFolderEntries(ctx context.Context, folder *os.File, offset int) error {
	for offset > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := folder.ReadDir(min(offset, 100))
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return domain.ErrFolderForbidden
		}
		offset -= len(entries)
	}
	return nil
}

func (s *WorkspaceService) AddWorkspace(ctx context.Context, request domain.AddWorkspaceRequest) (result domain.AddedWorkspace, err error) {
	ctx, span := scanTracer().Start(ctx, "workspace.add")
	defer span.End()
	span.AddEvent("execution.started")
	defer func() {
		if err != nil {
			telemetry.Fail(span, "workspace_registration_failed")
		} else {
			span.AddEvent("execution.completed")
		}
	}()
	path, err := s.selectedPath(request)
	if err != nil {
		return result, err
	}
	if _, err := source.Open(ctx, path, source.Options{}); err != nil {
		return result, gitRegistrationError(ctx)
	}
	cfg := s.defaults
	cfg.RepositoryPath, cfg.RepositoryName = path, strings.TrimSpace(request.Name)
	manager, reused, err := s.group.Add(ctx, cfg)
	if err != nil {
		return result, err
	}
	return domain.AddedWorkspace{Repository: manager.Repository(), AlreadyAdded: reused}, nil
}

func (s *WorkspaceService) ResolveRepository(ctx context.Context, request domain.ResolveRepositoryRequest) (result domain.RepositorySummary, err error) {
	ctx, span := scanTracer().Start(ctx, "workspace.resolve")
	defer span.End()
	path, err := repositoryLookupPath(request.Path)
	if err != nil {
		return result, err
	}
	if s.group == nil {
		return result, domain.ErrNotConfigured
	}
	manager := s.group.Find(repositoryID(path))
	if manager == nil {
		return result, domain.ErrNotFound
	}
	project, err := manager.Project(ctx)
	if err != nil {
		return result, err
	}
	span.SetAttributes(attribute.String("repository.id", project.Repository.ID))
	span.AddEvent("execution.completed")
	return domain.RepositorySummary{Repository: project.Repository, Snapshot: project.Snapshot}, nil
}

func repositoryLookupPath(path string) (string, error) {
	if path == "" || len(path) > 4096 || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return "", domain.ErrInvalidInput
	}
	cleaned := filepath.Clean(path)
	canonical, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		return canonical, nil
	}
	// Saved snapshots remain addressable while a checkout is temporarily absent,
	// but only the exact canonical path used at registration can resolve it.
	return cleaned, nil
}

func gitRegistrationError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return domain.ErrNotGitRepository
}

func (s *WorkspaceService) selectedPath(request domain.AddWorkspaceRequest) (string, error) {
	if !validFolderPath(request.Path) || len(request.Name) > 200 || strings.ContainsRune(request.Name, 0) {
		return "", domain.ErrInvalidInput
	}
	root, err := s.root(request.RootID)
	if err != nil {
		return "", err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root.Path, request.Path))
	if err != nil {
		return "", domain.ErrFolderForbidden
	}
	relative, err := filepath.Rel(root.Path, path)
	if err != nil || !filepath.IsLocal(relative) {
		return "", domain.ErrFolderForbidden
	}
	return path, nil
}
