package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

func normalizeOptions(opts Options) (Options, error) {
	if opts.MaxFileBytes < 0 || opts.MaxTotalBytes < 0 || opts.CaptureAttempts < 0 {
		return opts, errors.New("capture limits cannot be negative")
	}
	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = 4 << 20
	}
	if opts.MaxFileBytes > 1<<30 {
		return opts, errors.New("maximum file capture limit is 1 GiB")
	}
	if opts.MaxTotalBytes == 0 {
		opts.MaxTotalBytes = 64 << 20
	}
	if opts.CaptureAttempts == 0 {
		opts.CaptureAttempts = 3
	}
	if opts.CaptureAttempts > 10 {
		return opts, errors.New("capture attempts cannot exceed 10")
	}
	return opts, nil
}

func Open(ctx context.Context, root string, opts Options) (repo *Repository, err error) {
	ctx, span := tracer.Start(ctx, "source.open")
	defer func() { finishSpan(span, err) }()
	opts, err = normalizeOptions(opts)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	// Git interprets a terminal /* as a trust wildcard, even if it is a literal directory name.
	if strings.HasSuffix(root, "/*") {
		return nil, errors.New("repository root cannot end with a trust wildcard")
	}
	value, err := runGit(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, gitFailure("repository discovery", err)
	}
	// Remove only Git's record terminator; spaces and newlines can belong to a path.
	top := strings.TrimSuffix(string(value), "\n")
	top, err = filepath.EvalSymlinks(top)
	if err != nil {
		return nil, err
	}
	// A repository's core.worktree must not silently expand the caller's source boundary.
	if top != root {
		return nil, errors.New("repository path must be the working-tree root")
	}
	return &Repository{root: top, options: opts}, nil
}
