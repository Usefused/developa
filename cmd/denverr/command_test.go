package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestHelpAndVersionDoNotInitializeServices(t *testing.T) {
	var output strings.Builder
	if err := run(context.Background(), []string{"help"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "denverr serve") {
		t.Fatal("help omitted the server command")
	}
	output.Reset()
	if err := run(context.Background(), []string{"version"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "denverr dev") {
		t.Fatal("development version was not reported")
	}
}

func TestRunRejectsMissingAndUnknownCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"version", "extra"}} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted command %v", args)
		}
	}
}

func TestParseNativeServerAndWorkspaceOptions(t *testing.T) {
	serve, err := parseServeOptions([]string{"--database-url", "postgres://localhost/db", "--workspace-root", "/tmp"}, io.Discard)
	if err != nil || serve.databaseURL == "" || len(serve.roots) != 1 {
		t.Fatalf("unexpected serve options: %+v %v", serve, err)
	}
	workspace, err := parseWorkspaceAddOptions([]string{"--name", "Example", "/tmp/repo"}, io.Discard)
	if err != nil || workspace.name != "Example" || !strings.HasSuffix(workspace.path, "/tmp/repo") {
		t.Fatalf("unexpected workspace options: %+v %v", workspace, err)
	}
}

func TestCanonicalRootsDeduplicateAndRejectMissingFolders(t *testing.T) {
	dir := t.TempDir()
	roots, err := canonicalRoots([]string{dir, dir})
	if err != nil || len(roots) != 1 {
		t.Fatalf("roots were not canonicalized: %v %v", roots, err)
	}
	if _, err := canonicalRoots([]string{dir + "/missing"}); err == nil {
		t.Fatal("missing root was accepted")
	}
}
