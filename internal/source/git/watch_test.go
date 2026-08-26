package git

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchInitialAndRepeatedUpdates(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var updates []Update
	err := repo.Watch(ctx, time.Millisecond, func(_ context.Context, update Update) error {
		updates = append(updates, update)
		switch len(updates) {
		case 1:
			writeTestFile(t, dir, "main.go", "package main\n// first\n")
		case 2:
			writeTestFile(t, dir, "main.go", "package main\n// second\n")
		case 3:
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("watch ended unexpectedly: %v", err)
	}
	if len(updates) != 3 || updates[0].Previous != nil {
		t.Fatal("initial/update delivery mismatch")
	}
	assertChange(t, updates[0].Changes, "main.go", Added)
	assertChange(t, updates[1].Changes, "main.go", Modified)
	assertChange(t, updates[2].Changes, "main.go", Modified)
}

func TestWatchSkipsUnchangedSnapshotAndDeliversMetadata(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	commitTestFiles(t, dir)
	before := captureTest(t, repo)
	count := 0
	callback := func(_ context.Context, _ Update) error { count++; return nil }
	current, err := repo.watchIteration(context.Background(), before, callback)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || current != before {
		t.Fatal("unchanged snapshot was delivered")
	}
	gitTest(t, dir, "tag", "v1.2.3")
	current, err = repo.watchIteration(context.Background(), current, callback)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("metadata update was not delivered")
	}
	assertMetadataChange(t, before, current)
}

func TestWatchCallbackFailure(t *testing.T) {
	_, repo := testRepository(t)
	want := errors.New("callback failed")
	err := repo.Watch(context.Background(), time.Second, func(context.Context, Update) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("lost callback error: %v", err)
	}
}

func TestWatchInputValidation(t *testing.T) {
	_, repo := testRepository(t)
	if err := repo.Watch(context.Background(), 0, nil); err == nil {
		t.Fatal("zero interval accepted")
	}
	if err := repo.Watch(context.Background(), time.Second, nil); err == nil {
		t.Fatal("nil callback accepted")
	}
}
