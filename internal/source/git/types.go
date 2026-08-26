// Package git captures bounded working-tree snapshots without changing a checkout.
package git

import "errors"

var (
	ErrUnstable      = errors.New("repository changed during source capture")
	ErrLimitExceeded = errors.New("source capture size limit exceeded")
)

type Options struct {
	MaxFileBytes    int64
	MaxTotalBytes   int64
	CaptureAttempts int
}

type Repository struct {
	root    string
	options Options
}

func (r *Repository) Root() string { return r.root }

type File struct {
	Path    string `json:"path"`
	Content []byte `json:"-"`
	Hash    string `json:"hash"`
}

type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Snapshot owns its bytes. Callers must treat a published snapshot as immutable.
type Snapshot struct {
	Files            []File      `json:"files"`
	Fingerprint      string      `json:"fingerprint"`
	Commit           string      `json:"commit"`
	Branch           string      `json:"branch"`
	Dirty            bool        `json:"dirty"`
	Tags             []string    `json:"tags"`
	RefsFingerprint  string      `json:"refs_fingerprint"`
	IndexFingerprint string      `json:"index_fingerprint"`
	TrackedChanges   []Change    `json:"tracked_changes"`
	Exclusions       []Exclusion `json:"exclusions"`
	Complete         bool        `json:"complete"`
	stateFingerprint string
}

type ChangeKind string

const (
	Added    ChangeKind = "added"
	Modified ChangeKind = "modified"
	Deleted  ChangeKind = "deleted"
)

// Renames deliberately remain add/delete pairs: equal content does not prove identity.
type Change struct {
	Kind         ChangeKind `json:"kind"`
	Path         string     `json:"path"`
	PreviousHash string     `json:"previous_hash,omitempty"`
	Hash         string     `json:"hash,omitempty"`
}

type Update struct {
	Previous *Snapshot
	Current  *Snapshot
	Changes  []Change
}
