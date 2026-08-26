package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
)

func (r *Repository) Capture(ctx context.Context) (snapshot *Snapshot, err error) {
	ctx, span := tracer.Start(ctx, "source.capture")
	defer func() { finishSpan(span, err) }()
	root, err := os.OpenRoot(r.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for attempt := 0; attempt < r.options.CaptureAttempts; attempt++ {
		snapshot, err = r.capturePair(ctx, root)
		if !errors.Is(err, ErrUnstable) {
			break
		}
		span.AddEvent("retry")
	}
	if err == nil {
		span.SetAttributes(attribute.Int("source.files", len(snapshot.Files)),
			attribute.Int("source.exclusions", len(snapshot.Exclusions)), attribute.Bool("source.complete", snapshot.Complete))
	}
	return snapshot, err
}

func (r *Repository) capturePair(ctx context.Context, root *os.Root) (*Snapshot, error) {
	first, err := r.readSnapshot(ctx, root)
	if err != nil {
		return nil, err
	}
	second, err := r.readSnapshot(ctx, root)
	if err != nil {
		return nil, err
	}
	// Git status alone cannot detect repeated edits to an already dirty file.
	// Matching complete manifests provides optimistic consistency without locking the checkout.
	if first.Fingerprint != second.Fingerprint {
		return nil, ErrUnstable
	}
	return second, nil
}

func (r *Repository) readSnapshot(ctx context.Context, root *os.Root) (*Snapshot, error) {
	state, err := r.readInventory(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := state.snapshot
	budget := captureBudget{}
	for _, entry := range state.candidates {
		if err := r.captureCandidate(ctx, root, &snapshot, entry, &budget); err != nil {
			return nil, err
		}
	}
	snapshot.Fingerprint = snapshotFingerprint(&snapshot)
	return &snapshot, nil
}

type captureBudget struct {
	used      int64
	exhausted bool
}

func (r *Repository) captureCandidate(ctx context.Context, root *os.Root, snapshot *Snapshot, entry candidate, budget *captureBudget) error {
	exclusion, complete := candidateExclusion(entry)
	if exclusion != "" {
		appendExclusion(snapshot, entry.path, exclusion, complete)
		return nil
	}
	// The Go indexer only consumes Go source and module boundaries. Ignoring
	// unrelated tracked assets keeps a monorepo's images and build artifacts
	// from exhausting the source budget before useful code is reached.
	if !captureSourcePath(entry.path) {
		return nil
	}
	if budget.exhausted {
		appendExclusion(snapshot, entry.path, "total_size_limit", false)
		return nil
	}
	file, exclusion, complete, err := r.readCandidate(ctx, root, entry)
	if err != nil {
		return err
	}
	if exclusion != "" {
		appendExclusion(snapshot, entry.path, exclusion, complete)
	}
	if file == nil {
		return nil
	}
	if budget.used+int64(len(file.Content)) > r.options.MaxTotalBytes {
		appendExclusion(snapshot, entry.path, "total_size_limit", false)
		budget.exhausted = true
		return nil
	}
	budget.used += int64(len(file.Content))
	snapshot.Files = append(snapshot.Files, *file)
	return nil
}

func appendExclusion(snapshot *Snapshot, path, reason string, complete bool) {
	snapshot.Exclusions = append(snapshot.Exclusions, Exclusion{Path: path, Reason: reason})
	snapshot.Complete = snapshot.Complete && complete
}

func snapshotFingerprint(snapshot *Snapshot) string {
	values := []string{snapshot.stateFingerprint, strconv.FormatBool(snapshot.Complete)}
	for _, file := range snapshot.Files {
		values = append(values, file.Path, file.Hash)
	}
	for _, exclusion := range snapshot.Exclusions {
		values = append(values, exclusion.Path, exclusion.Reason)
	}
	return fingerprint(values...)
}

func (r *Repository) readCandidate(ctx context.Context, root *os.Root, entry candidate) (*File, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", false, err
	}
	symlink, err := hasSymlink(root, entry.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", true, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	if symlink {
		return nil, "symlink_policy", true, nil
	}
	file, err := r.readFile(ctx, root, entry.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, ErrUnstable
	}
	if errors.Is(err, ErrLimitExceeded) {
		return nil, "file_size_limit", false, nil
	}
	return file, "", true, err
}

func (r *Repository) readFile(ctx context.Context, root *os.Root, name string) (*File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("source entry is not a regular file")
	}
	if before.Size() > r.options.MaxFileBytes {
		return nil, ErrLimitExceeded
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, r.options.MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > r.options.MaxFileBytes {
		return nil, ErrLimitExceeded
	}
	if err := verifyFile(root, file, name, before); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return &File{Path: name, Content: data, Hash: hex.EncodeToString(hash[:])}, nil
}

func verifyFile(root *os.Root, file *os.File, name string, before os.FileInfo) error {
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return ErrUnstable
	}
	symlink, err := hasSymlink(root, name)
	if err != nil {
		return ErrUnstable
	}
	if symlink {
		return ErrUnstable
	}
	current, err := root.Lstat(name)
	if err != nil {
		return ErrUnstable
	}
	if !os.SameFile(current, after) {
		return ErrUnstable
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
