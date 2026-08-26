package localconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateTokenCreatesAndReusesPrivateCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "api-token")
	created, err := loadOrCreateToken(path, bytes.NewReader(bytes.Repeat([]byte{0xab}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.Value != strings.Repeat("ab", 32) {
		t.Fatalf("unexpected generated token: %+v", created)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential is not private: %v %v", info, err)
	}
	reused, err := loadOrCreateToken(path, bytes.NewReader(nil))
	if err != nil || reused.Created || reused.Value != created.Value {
		t.Fatalf("credential was not reused: %+v %v", reused, err)
	}
}

func TestLoadOrCreateTokenRejectsUnsafeCredentials(t *testing.T) {
	for _, value := range []string{"short", strings.Repeat("x", 24) + " space", strings.Repeat("x", maxTokenBytes+1)} {
		if err := validateToken(value); err == nil {
			t.Fatalf("accepted unsafe token length %d", len(value))
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "api-token")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 24)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err == nil {
		t.Fatal("accepted group-readable credential")
	}
}

func TestLoadOrCreateTokenRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "api-token")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 24)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := readToken(link); err == nil {
		t.Fatal("accepted symlinked credential")
	}
}
