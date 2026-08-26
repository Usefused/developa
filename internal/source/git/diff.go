package git

import "sort"

// Diff compares captured content, not HEAD, so every dirty-to-dirty edit is visible.
// Ref/index-only changes have an empty diff but a different snapshot fingerprint.
func Diff(previous, current *Snapshot) []Change {
	before, after := fileManifest(previous), fileManifest(current)
	changes := make([]Change, 0)
	for path, hash := range after {
		old, exists := before[path]
		if !exists {
			changes = append(changes, Change{Kind: Added, Path: path, Hash: hash})
			continue
		}
		if old != hash {
			changes = append(changes, Change{Kind: Modified, Path: path, PreviousHash: old, Hash: hash})
		}
	}
	for path, hash := range before {
		if _, exists := after[path]; !exists {
			changes = append(changes, Change{Kind: Deleted, Path: path, PreviousHash: hash})
		}
	}
	sortChanges(changes)
	return changes
}

func fileManifest(snapshot *Snapshot) map[string]string {
	files := make(map[string]string)
	if snapshot == nil {
		return files
	}
	for _, file := range snapshot.Files {
		files[file.Path] = file.Hash
	}
	return files
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
}
