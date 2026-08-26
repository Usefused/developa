package localconfig

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

const maxTokenBytes = 8192

type Token struct {
	Value   string
	Path    string
	Created bool
}

// LoadOrCreateToken keeps the operator credential outside the repository and avoids process-list flags.
func LoadOrCreateToken(explicit string) (Token, error) {
	if explicit != "" {
		return Token{Value: explicit}, validateToken(explicit)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return Token{}, errors.New("user configuration directory is unavailable")
	}
	return loadOrCreateToken(filepath.Join(dir, "denverr", "api-token"), rand.Reader)
}

func loadOrCreateToken(path string, random io.Reader) (Token, error) {
	token, err := readToken(path)
	if err == nil {
		return Token{Value: token, Path: path}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Token{}, err
	}
	return createToken(path, random)
}

func readToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || insecurePermissions(info.Mode()) || info.Size() > maxTokenBytes+1 {
		return "", errors.New("Denverr credential file must be a private regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("Denverr credential file is unreadable")
	}
	token := strings.TrimSuffix(string(content), "\n")
	return token, validateToken(token)
}

func createToken(path string, random io.Reader) (Token, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Token{}, errors.New("Denverr configuration directory could not be created")
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(random, raw); err != nil {
		return Token{}, errors.New("Denverr access token could not be generated")
	}
	value := hex.EncodeToString(raw)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateToken(path, random)
	}
	if err != nil {
		return Token{}, errors.New("Denverr credential file could not be created")
	}
	if err := writeToken(file, value); err != nil {
		_ = os.Remove(path)
		return Token{}, err
	}
	return Token{Value: value, Path: path, Created: true}, nil
}

func writeToken(file *os.File, value string) error {
	if _, err := io.WriteString(file, value+"\n"); err != nil {
		file.Close()
		return errors.New("Denverr credential file could not be written")
	}
	if err := file.Close(); err != nil {
		return errors.New("Denverr credential file could not be written")
	}
	return nil
}

func validateToken(token string) error {
	if len(token) < 24 || len(token) > maxTokenBytes || strings.IndexFunc(token, unicode.IsSpace) >= 0 || strings.ContainsRune(token, 0) {
		return errors.New("Denverr access token must contain 24 to 8192 non-whitespace bytes")
	}
	return nil
}

func insecurePermissions(mode os.FileMode) bool {
	return runtime.GOOS != "windows" && mode.Perm()&0o077 != 0
}
