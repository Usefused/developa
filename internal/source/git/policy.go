package git

import (
	"io/fs"
	"os"
	"path"
	"strings"
)

var secretNames = map[string]bool{
	".env": true, ".npmrc": true, ".pypirc": true, ".netrc": true, ".git-credentials": true,
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	"credentials": true, "credentials.json": true, "secrets.json": true, "secrets.yaml": true,
	"secrets.yml": true, "secrets.toml": true, "secrets.env": true,
}

var secretExtensions = map[string]bool{".pem": true, ".key": true, ".p12": true, ".pfx": true, ".keystore": true}
var excludedDirectories = map[string]bool{".git": true, ".ssh": true, ".aws": true, ".kube": true, ".docker": true, "secrets": true}

func secretPath(name string) bool {
	lower := strings.ToLower(name)
	base := path.Base(lower)
	if secretNames[base] || secretExtensions[path.Ext(base)] || strings.HasPrefix(base, ".env.") {
		return true
	}
	for _, part := range strings.Split(lower, "/") {
		if excludedDirectories[part] {
			return true
		}
	}
	return false
}

func candidateExclusion(entry candidate) (string, bool) {
	if !fs.ValidPath(entry.path) {
		return "unsafe_path", false
	}
	if secretPath(entry.path) {
		return "secret_policy", true
	}
	if entry.mode == "120000" {
		return "symlink_policy", true
	}
	if entry.mode == "160000" {
		return "unsupported_submodule", false
	}
	if entry.conflict {
		return "unmerged_path", false
	}
	return "", true
}

func hasSymlink(root *os.Root, name string) (bool, error) {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}
