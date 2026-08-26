package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureRejectsChangingCheckout(t *testing.T) {
	dir, repo := testRepository(t)
	writeTestFile(t, dir, "main.go", "package main\n")
	repo.options.CaptureAttempts = 1
	ctx, cancel := context.WithCancel(context.Background())
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- mutateSource(ctx, dir) }()
	_, err := repo.Capture(context.Background())
	cancel()
	if writerErr := <-errorsCh; writerErr != nil {
		t.Fatal(writerErr)
	}
	if !errors.Is(err, ErrUnstable) {
		t.Fatalf("changing checkout was published: %v", err)
	}
}

func mutateSource(ctx context.Context, dir string) error {
	ticker := time.NewTicker(3 * time.Millisecond)
	defer ticker.Stop()
	for revision := 0; ; revision++ {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := replaceSource(dir, revision); err != nil {
				return err
			}
		}
	}
}

func replaceSource(dir string, revision int) error {
	next := filepath.Join(dir, ".git", "capture-next")
	data := []byte(fmt.Sprintf("package main\n// revision %d\n", revision))
	if err := os.WriteFile(next, data, 0600); err != nil {
		return err
	}
	return os.Rename(next, filepath.Join(dir, "main.go"))
}
