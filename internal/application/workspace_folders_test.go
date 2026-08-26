package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"developa/internal/domain"
)

func folderFixture(t *testing.T) (*WorkspaceService, string, string) {
	t.Helper()
	path := t.TempDir()
	service, err := NewWorkspaceService(nil, ManagerConfig{}, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	roots, _ := service.FolderRoots(context.Background())
	return service, roots[0].ID, path
}

func TestWorkspaceFoldersRejectEscapesAndNonGitWithoutInitializingGit(t *testing.T) {
	service, id, path := folderFixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(path, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"../", "/etc", "escape", "missing"} {
		if _, err := service.Folders(context.Background(), id, relative, 0); err == nil {
			t.Fatal("unsafe filesystem listing accepted")
		}
		if _, err := service.AddWorkspace(context.Background(), domain.AddWorkspaceRequest{RootID: id, Path: relative}); err == nil {
			t.Fatal("unsafe registration accepted")
		}
	}
	_, err := service.AddWorkspace(context.Background(), domain.AddWorkspaceRequest{RootID: id, Path: "."})
	if !errors.Is(err, domain.ErrNotGitRepository) {
		t.Fatal("missing Git not explained", err)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("registration modified the selected folder")
	}
}

func TestWorkspaceFoldersBoundDirectoryReadsAndHideGitMetadata(t *testing.T) {
	service, id, path := folderFixture(t)
	for i := 0; i < 105; i++ {
		if err := os.Mkdir(filepath.Join(path, fmt.Sprintf("folder-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(path, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	seen := collectFolderPages(t, service, id)
	if len(seen) != 105 || seen[".git"] {
		t.Fatal("folder pages lost entries or exposed Git metadata")
	}
}

func collectFolderPages(t *testing.T, service *WorkspaceService, id string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	offset := 0
	for {
		page, err := service.Folders(context.Background(), id, ".", offset)
		if err != nil || len(page.Items) > 100 {
			t.Fatal("unbounded or failed folder page", err)
		}
		for _, folder := range page.Items {
			seen[folder.Name] = true
		}
		if page.NextOffset == nil {
			break
		}
		offset = *page.NextOffset
	}
	return seen
}

func TestWorkspaceFolderValidation(t *testing.T) {
	service, id, _ := folderFixture(t)
	for _, offset := range []int{-1, 100001} {
		if _, err := service.Folders(context.Background(), id, ".", offset); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatal("unbounded offset accepted")
		}
	}
	if _, err := service.Folders(context.Background(), "unknown", ".", 0); !errors.Is(err, domain.ErrFolderForbidden) {
		t.Fatal("unknown root accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Folders(ctx, id, ".", 0); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled folder browse succeeded")
	}
}

func TestWorkspaceRegistrationCannotPublishAnUnconfiguredManager(t *testing.T) {
	err := persistAddedWorkspace(context.Background(), nil, &Manager{}, "")
	if !errors.Is(err, domain.ErrFolderForbidden) {
		t.Fatal("unconfigured manager reached durable registration")
	}
}

func TestWorkspaceResolutionUsesCanonicalExactRepositoryRoots(t *testing.T) {
	group, _, roots := workspaceFixture(t, nil)
	service, err := NewWorkspaceService(group, ManagerConfig{}, roots)
	if err != nil {
		t.Fatal(err)
	}
	want := group.Managers()[0].Repository()
	resolved, err := service.ResolveRepository(context.Background(), domain.ResolveRepositoryRequest{Path: roots[0]})
	if err != nil || resolved.Repository != want {
		t.Fatal("registered root did not resolve", err, resolved)
	}
	alias := filepath.Join(t.TempDir(), "repository-alias")
	if err := os.Symlink(roots[0], alias); err != nil {
		t.Fatal(err)
	}
	resolved, err = service.ResolveRepository(context.Background(), domain.ResolveRepositoryRequest{Path: alias})
	if err != nil || resolved.Repository != want {
		t.Fatal("canonical alias did not resolve", err, resolved)
	}
	cases := []struct {
		path string
		err  error
	}{
		{path: ".", err: domain.ErrInvalidInput},
		{path: filepath.Join(roots[0], "internal"), err: domain.ErrNotFound},
		{path: filepath.Join(t.TempDir(), "unknown"), err: domain.ErrNotFound},
	}
	for _, test := range cases {
		_, err := service.ResolveRepository(context.Background(), domain.ResolveRepositoryRequest{Path: test.path})
		if !errors.Is(err, test.err) {
			t.Fatalf("resolution error for %q = %v, want %v", test.path, err, test.err)
		}
	}
}
