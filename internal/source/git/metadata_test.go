package git

import (
	"context"
	"testing"
)

func TestBranchDetachedHEADAndTagUpdates(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	initial := captureTest(t, repo)
	gitTest(t, dir, "tag", "v1.0.0")
	tagged := captureTest(t, repo)
	if len(tagged.Tags) != 1 || tagged.Tags[0] != "v1.0.0" {
		t.Fatal("tag evidence missing")
	}
	assertMetadataChange(t, initial, tagged)
	gitTest(t, dir, "checkout", "--quiet", "-b", "feature")
	branched := captureTest(t, repo)
	if branched.Branch != "feature" {
		t.Fatal("branch switch missed")
	}
	assertMetadataChange(t, tagged, branched)
	gitTest(t, dir, "checkout", "--quiet", "--detach", initial.Commit)
	detached := captureTest(t, repo)
	if detached.Branch != "" {
		t.Fatal("detached HEAD reported a branch")
	}
	assertMetadataChange(t, branched, detached)
}

func TestMergeConflictIsExplicitlyIncomplete(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package initial\n")
	commitTestFiles(t, dir)
	gitTest(t, dir, "checkout", "--quiet", "-b", "other")
	writeTestFile(t, dir, "main.go", "package other\n")
	commitTestFiles(t, dir)
	gitTest(t, dir, "checkout", "--quiet", "main")
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	if _, err := runGit(context.Background(), dir, "merge", "--no-edit", "other"); err == nil {
		t.Fatal("fixture did not conflict")
	}
	snapshot := captureTest(t, repo)
	assertExcluded(t, snapshot, "main.go", "unmerged_path")
	if snapshot.Complete || len(snapshot.Files) != 0 {
		t.Fatal("conflicted source was presented as analyzed")
	}
}

func assertMetadataChange(t *testing.T, before, after *Snapshot) {
	t.Helper()
	if before.Fingerprint == after.Fingerprint {
		t.Fatal("metadata change missed")
	}
	if len(Diff(before, after)) != 0 {
		t.Fatal("metadata-only change reported a source change")
	}
}

func TestSubmodulePointerIsExplicitlyIncomplete(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	first := captureTest(t, repo)
	gitTest(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+first.Commit+",module")
	snapshot := captureTest(t, repo)
	assertExcluded(t, snapshot, "module", "unsupported_submodule")
	if snapshot.Complete {
		t.Fatal("submodule analysis was reported complete")
	}
	if snapshot.IndexFingerprint == first.IndexFingerprint {
		t.Fatal("submodule pointer update missed")
	}
}

func TestRefUpdateAwayFromHEAD(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	first := captureTest(t, repo)
	gitTest(t, dir, "branch", "another-ref")
	second := captureTest(t, repo)
	assertMetadataChange(t, first, second)
}
