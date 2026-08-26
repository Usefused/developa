package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type candidate struct {
	path     string
	mode     string
	conflict bool
}

type inventory struct {
	snapshot   Snapshot
	candidates []candidate
}

func fingerprint(values ...string) string {
	h := sha256.New()
	for _, value := range values {
		h.Write([]byte(strconv.Itoa(len(value)) + ":"))
		h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *Repository) readMetadata(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	commit, err := optionalGit(ctx, r.root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return snapshot, gitFailure("HEAD", err)
	}
	branch, err := optionalGit(ctx, r.root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return snapshot, gitFailure("branch", err)
	}
	refs, err := runGit(ctx, r.root, "for-each-ref", "--format=%(refname)%00%(objectname)")
	if err != nil {
		return snapshot, gitFailure("refs", err)
	}
	snapshot.Commit, snapshot.Branch = strings.TrimSpace(string(commit)), strings.TrimSpace(string(branch))
	snapshot.RefsFingerprint = fingerprint(string(refs))
	snapshot.Complete = true
	if snapshot.Commit != "" {
		tags, err := runGit(ctx, r.root, "tag", "--points-at", snapshot.Commit)
		if err != nil {
			return snapshot, gitFailure("tags", err)
		}
		snapshot.Tags = strings.Fields(string(tags))
	}
	return snapshot, nil
}

func (r *Repository) readInventory(ctx context.Context) (inventory, error) {
	var result inventory
	snapshot, err := r.readMetadata(ctx)
	if err != nil {
		return result, err
	}
	indexed, err := runGit(ctx, r.root, "ls-files", "--stage", "-z", "--")
	if err != nil {
		return result, gitFailure("tracked files", err)
	}
	untracked, err := runGit(ctx, r.root, "ls-files", "--others", "--exclude-standard", "-z", "--")
	if err != nil {
		return result, gitFailure("untracked files", err)
	}
	status, err := runGit(ctx, r.root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=dirty")
	if err != nil {
		return result, gitFailure("status", err)
	}
	snapshot.TrackedChanges, err = r.trackedChanges(ctx, snapshot.Commit)
	if err != nil {
		return result, err
	}
	result.candidates, err = parseCandidates(indexed, untracked)
	if err != nil {
		return result, err
	}
	snapshot.Dirty = len(status) != 0
	snapshot.IndexFingerprint = fingerprint(string(indexed))
	snapshot.stateFingerprint = fingerprint(snapshot.Commit, snapshot.Branch, snapshot.RefsFingerprint,
		snapshot.IndexFingerprint, string(untracked), string(status))
	result.snapshot = snapshot
	return result, nil
}

func parseCandidates(indexed, untracked []byte) ([]candidate, error) {
	byPath := make(map[string]candidate)
	for _, line := range strings.Split(string(indexed), "\x00") {
		if line == "" {
			continue
		}
		entry, err := parseIndexEntry(line)
		if err != nil {
			return nil, err
		}
		entry.conflict = entry.conflict || byPath[entry.path].conflict
		byPath[entry.path] = entry
	}
	for _, path := range strings.Split(string(untracked), "\x00") {
		if path != "" {
			byPath[path] = candidate{path: path}
		}
	}
	result := make([]candidate, 0, len(byPath))
	for _, entry := range byPath {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].path < result[j].path })
	return result, nil
}

func parseIndexEntry(line string) (candidate, error) {
	header, path, ok := strings.Cut(line, "\t")
	fields := strings.Fields(header)
	if !ok || len(fields) != 3 {
		return candidate{}, fmt.Errorf("invalid Git index record")
	}
	return candidate{path: path, mode: fields[0], conflict: fields[2] != "0"}, nil
}

func (r *Repository) trackedChanges(ctx context.Context, commit string) ([]Change, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z", "--ignore-submodules=dirty"}
	if commit != "" {
		data, err := runGit(ctx, r.root, append(args, commit, "--")...)
		if err != nil {
			return nil, gitFailure("diff", err)
		}
		return parseGitChanges(data)
	}
	staged, err := runGit(ctx, r.root, append(args, "--cached", "--")...)
	if err != nil {
		return nil, gitFailure("staged diff", err)
	}
	unstaged, err := runGit(ctx, r.root, append(args, "--")...)
	if err != nil {
		return nil, gitFailure("unstaged diff", err)
	}
	return parseGitChanges(append(staged, unstaged...))
}

func parseGitChanges(data []byte) ([]Change, error) {
	fields := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if len(data) == 0 {
		return nil, nil
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("invalid Git diff record")
	}
	byPath := make(map[string]Change)
	for i := 0; i < len(fields); i += 2 {
		kind := Modified
		switch fields[i] {
		case "A":
			kind = Added
		case "D":
			kind = Deleted
		}
		byPath[fields[i+1]] = Change{Kind: kind, Path: fields[i+1]}
	}
	result := make([]Change, 0, len(byPath))
	for _, change := range byPath {
		result = append(result, change)
	}
	sortChanges(result)
	return result, nil
}
