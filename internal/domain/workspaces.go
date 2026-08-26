package domain

import (
	"context"
	"errors"
)

var (
	ErrNotGitRepository = errors.New("selected folder is not a Git repository root")
	ErrWorkspaceLimit   = errors.New("workspace limit reached")
	ErrFolderForbidden  = errors.New("folder is outside configured workspace roots")
)

// Server paths are durable operator configuration, never part of the public repository catalog.
type WorkspaceRegistration struct {
	Repository
	Root     string    `json:"root"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
}

type WorkspaceStore interface {
	SaveWorkspaces(context.Context, []WorkspaceRegistration, Execution) error
	SavedWorkspaces(context.Context, []string) ([]WorkspaceRegistration, error)
}

type FolderRoot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Folder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type FolderPage struct {
	RootID     string   `json:"root_id"`
	Path       string   `json:"path"`
	Items      []Folder `json:"items"`
	NextOffset *int     `json:"next_offset"`
}

type AddWorkspaceRequest struct {
	RootID string `json:"root_id"`
	Path   string `json:"path"`
	Name   string `json:"name"`
}

type AddedWorkspace struct {
	Repository
	AlreadyAdded bool `json:"already_added"`
}

type ResolveRepositoryRequest struct {
	Path string `json:"path"`
}

type WorkspaceManagement interface {
	FolderRoots(context.Context) ([]FolderRoot, error)
	Folders(context.Context, string, string, int) (FolderPage, error)
	AddWorkspace(context.Context, AddWorkspaceRequest) (AddedWorkspace, error)
	ResolveRepository(context.Context, ResolveRepositoryRequest) (RepositorySummary, error)
}
