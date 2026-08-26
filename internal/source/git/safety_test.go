package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitTrustIsScopedToSelectedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "selected repository")
	args := gitArguments(root, []string{"status"})
	var trusted []string
	for index, argument := range args {
		if argument != "-c" || index+1 >= len(args) {
			continue
		}
		if value, ok := strings.CutPrefix(args[index+1], "safe.directory="); ok {
			trusted = append(trusted, value)
		}
	}
	if !reflect.DeepEqual(trusted, []string{"", root}) {
		t.Fatalf("trust escaped selected root: %q", trusted)
	}
}

func TestOpenRejectsWildcardNamedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "*")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), root, Options{}); err == nil {
		t.Fatal("Git trust wildcard accepted as a root")
	}
}

func TestGitHelpersAreDisabled(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	writeTestFile(t, dir, ".gitattributes", "*.go diff=unsafe filter=unsafe\n")
	commitTestFiles(t, dir)
	helper, marker := installUnsafeHelper(t)
	gitTest(t, dir, "config", "diff.external", helper)
	gitTest(t, dir, "config", "diff.unsafe.textconv", helper)
	gitTest(t, dir, "config", "core.fsmonitor", helper)
	gitTest(t, dir, "config", "filter.unsafe.clean", helper)
	gitTest(t, dir, "config", "filter.unsafe.process", helper)
	writeTestFile(t, dir, "main.go", "package main\n// modified\n")
	captureTest(t, repo)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git helper executed: %v", err)
	}
}

func installUnsafeHelper(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	helper, marker := filepath.Join(dir, "helper"), filepath.Join(dir, "called")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n: > \"$DEVELOPA_TEST_MARKER\"\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVELOPA_TEST_MARKER", marker)
	return helper, marker
}

func TestGitEnvironmentCannotRedirectRepository(t *testing.T) {
	dir, _ := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	other, _ := testRepository(t)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	repo, err := Open(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := captureTest(t, repo)
	assertFileContent(t, snapshot, "main.go", "package main\n")
}

func TestOpenRejectsUnexpectedWorkingTree(t *testing.T) {
	dir, _ := testRepository(t)
	outside := t.TempDir()
	gitTest(t, dir, "config", "core.worktree", outside)
	if _, err := Open(context.Background(), dir, Options{}); err == nil {
		t.Fatal("repository configuration expanded source boundary")
	}
}

func TestSymlinkParentIsExcluded(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "sub/main.go", "package main\n")
	commitTestFiles(t, dir)
	if err := os.Rename(filepath.Join(dir, "sub"), filepath.Join(dir, "saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("saved", filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	snapshot := captureTest(t, repo)
	assertExcluded(t, snapshot, "sub/main.go", "symlink_policy")
}

func TestVerifyFileDetectsModification(t *testing.T) {
	dir, _ := testRepository(t)
	writeTestFile(t, dir, "main.go", "short")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.Open("main.go")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, dir, "main.go", "different and longer")
	if err := verifyFile(root, file, "main.go", before); !errors.Is(err, ErrUnstable) {
		t.Fatalf("missed source mutation: %v", err)
	}
}

func TestInvalidPathsAndIndexRecords(t *testing.T) {
	cases := []string{"../outside.go", "/outside.go", "a/../../outside.go"}
	for _, path := range cases {
		reason, complete := candidateExclusion(candidate{path: path})
		if reason != "unsafe_path" || complete {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	if _, err := parseIndexEntry("broken"); err == nil {
		t.Fatal("malformed index record accepted")
	}
	if _, err := parseGitChanges([]byte("M\x00")); err == nil {
		t.Fatal("malformed diff accepted")
	}
}
