package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testRepository(t *testing.T) (string, *Repository) {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "-c", "init.defaultBranch=main", "init", "--quiet")
	gitTest(t, dir, "config", "user.name", "Denverr Test")
	gitTest(t, dir, "config", "user.email", "developa@example.invalid")
	repo, err := Open(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return dir, repo
}

func gitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := runGit(context.Background(), root, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(output)
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func commitTestFiles(t *testing.T, root string) {
	t.Helper()
	gitTest(t, root, "add", "--all")
	gitTest(t, root, "commit", "--quiet", "-m", "fixture")
}

func captureTest(t *testing.T, repo *Repository) *Snapshot {
	t.Helper()
	snapshot, err := repo.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertChange(t *testing.T, changes []Change, path string, kind ChangeKind) {
	t.Helper()
	for _, change := range changes {
		if change.Path == path && change.Kind == kind {
			return
		}
	}
	t.Fatalf("missing %s change for %q: %+v", kind, path, changes)
}

func assertFileContent(t *testing.T, snapshot *Snapshot, path, content string) {
	t.Helper()
	for _, file := range snapshot.Files {
		if file.Path != path {
			continue
		}
		if string(file.Content) != content {
			t.Fatalf("wrong content for %q", path)
		}
		return
	}
	t.Fatalf("file %q not captured", path)
}

func assertExcluded(t *testing.T, snapshot *Snapshot, path, reason string) {
	t.Helper()
	for _, exclusion := range snapshot.Exclusions {
		if exclusion.Path == path && exclusion.Reason == reason {
			return
		}
	}
	t.Fatalf("missing exclusion for %q: %+v", path, snapshot.Exclusions)
}
