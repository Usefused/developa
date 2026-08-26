package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRepeatedDirtyEdits(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	first := captureTest(t, repo)
	writeTestFile(t, dir, "main.go", "package main\n// edit one\n")
	second := captureTest(t, repo)
	writeTestFile(t, dir, "main.go", "package main\n// edit two\n")
	third := captureTest(t, repo)
	assertChange(t, Diff(first, second), "main.go", Modified)
	assertChange(t, Diff(second, third), "main.go", Modified)
	if second.Commit != third.Commit || !third.Dirty {
		t.Fatal("dirty edits changed HEAD or were missed")
	}
	if second.Fingerprint == third.Fingerprint {
		t.Fatal("dirty-to-dirty update was missed")
	}
	assertFileContent(t, first, "main.go", "package main\n")
}

func TestUnbornAndSpecialFilenames(t *testing.T) {
	dir, repo := testRepository(t)
	names := []string{"space name.go", "line\nbreak.go", "-dash.go"}
	for _, name := range names {
		writeTestFile(t, dir, name, "package example\n")
	}
	first := captureTest(t, repo)
	if first.Commit != "" || first.Branch != "main" || len(first.Files) != 3 {
		t.Fatal("unexpected unborn snapshot")
	}
	for _, name := range names {
		assertFileContent(t, first, name, "package example\n")
	}
	gitTest(t, dir, "add", "--all")
	second := captureTest(t, repo)
	if len(Diff(first, second)) != 0 {
		t.Fatal("staging changed source content")
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("staging was not reflected in metadata")
	}
	for _, name := range names {
		assertChange(t, second.TrackedChanges, name, Added)
	}
}

func TestIgnoredSecretAndSymlinkPolicies(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, ".gitignore", "*.ignored\n")
	writeTestFile(t, dir, "main.go", "package main\n")
	writeTestFile(t, dir, "hidden.ignored", "ignored")
	writeTestFile(t, dir, ".env", "SECRET=excluded")
	writeTestFile(t, dir, "private.key", "excluded")
	outside := t.TempDir()
	writeTestFile(t, outside, "other.go", "outside")
	if err := os.Symlink(filepath.Join(outside, "other.go"), filepath.Join(dir, "link.go")); err != nil {
		t.Fatal(err)
	}
	commitTestFiles(t, dir)
	snapshot := captureTest(t, repo)
	if len(snapshot.Files) != 2 || !snapshot.Complete {
		t.Fatalf("unexpected files/completeness: %+v", snapshot)
	}
	assertExcluded(t, snapshot, ".env", "secret_policy")
	assertExcluded(t, snapshot, "private.key", "secret_policy")
	assertExcluded(t, snapshot, "link.go", "symlink_policy")
}

func TestDeletionAndConservativeRename(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "before.go", "package example\n")
	commitTestFiles(t, dir)
	before := captureTest(t, repo)
	if err := os.Rename(filepath.Join(dir, "before.go"), filepath.Join(dir, "after.go")); err != nil {
		t.Fatal(err)
	}
	after := captureTest(t, repo)
	changes := Diff(before, after)
	assertChange(t, changes, "before.go", Deleted)
	assertChange(t, changes, "after.go", Added)
	assertChange(t, after.TrackedChanges, "before.go", Deleted)
	if len(changes) != 2 {
		t.Fatalf("unexpected rename changes: %+v", changes)
	}
}

func TestStagedAndUnstagedChanges(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	writeTestFile(t, dir, "main.go", "package main\n// staged\n")
	gitTest(t, dir, "add", "main.go")
	writeTestFile(t, dir, "main.go", "package main\n// unstaged\n")
	snapshot := captureTest(t, repo)
	assertFileContent(t, snapshot, "main.go", "package main\n// unstaged\n")
	assertChange(t, snapshot.TrackedChanges, "main.go", Modified)
	if !snapshot.Dirty {
		t.Fatal("working tree was reported clean")
	}
}

func TestCaptureLimits(t *testing.T) {
	cases := []struct {
		name    string
		options Options
	}{
		{"file", Options{MaxFileBytes: 2}},
		{"total", Options{MaxTotalBytes: 3}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir, _ := testRepository(t)
			writeTestFile(t, dir, "a.go", "abc")
			writeTestFile(t, dir, "b.go", "abc")
			repo, err := Open(context.Background(), dir, test.options)
			if err != nil {
				t.Fatal(err)
			}
			_, err = repo.Capture(context.Background())
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("expected bounded capture, got %v", err)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	dir, repo := testRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Capture(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("capture: %v", err)
	}
	if _, err := Open(ctx, dir, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("open: %v", err)
	}
}
