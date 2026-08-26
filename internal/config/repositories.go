package config

import (
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Repository struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func loadRepositories(lookup lookupEnv, cfg *Config) error {
	raw, _ := lookup("REPOSITORIES")
	if len(raw) > 64<<10 || !utf8.ValidString(raw) {
		return errors.New("REPOSITORIES must be UTF-8 JSON no greater than 64 KiB")
	}
	if strings.TrimSpace(raw) == "" {
		if cfg.RepositoryPath != "" {
			cfg.Repositories = []Repository{{Name: cfg.RepositoryName, Path: cfg.RepositoryPath}}
		}
		return validateRepositories(cfg.Repositories, false)
	}
	if cfg.RepositoryPath != "" || cfg.RepositoryName != "" {
		return errors.New("REPOSITORIES cannot be combined with REPOSITORY_PATH or REPOSITORY_NAME")
	}
	repositories, err := decodeRepositories(raw)
	if err != nil {
		return errors.New("REPOSITORIES must be an array of at most 32 objects containing only name and path strings")
	}
	cfg.Repositories = repositories
	return validateRepositories(repositories, true)
}

func decodeRepositories(raw string) ([]Repository, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('[') {
		return nil, errors.New("invalid repositories array")
	}
	repositories := []Repository{}
	for decoder.More() {
		if len(repositories) >= 32 {
			return nil, errors.New("repository limit exceeded")
		}
		repository, err := decodeRepository(decoder)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing repositories data")
	}
	return repositories, nil
}

func decodeRepository(decoder *json.Decoder) (Repository, error) {
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return Repository{}, errors.New("invalid repository object")
	}
	values := map[string]string{}
	for decoder.More() {
		if err := decodeRepositoryField(decoder, values); err != nil {
			return Repository{}, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Repository{}, err
	}
	return Repository{Name: values["name"], Path: values["path"]}, nil
}

func decodeRepositoryField(decoder *json.Decoder, values map[string]string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	key, ok := token.(string)
	if !ok || key != "name" && key != "path" {
		return errors.New("unknown repository field")
	}
	if _, duplicate := values[key]; duplicate {
		return errors.New("duplicate repository field")
	}
	token, err = decoder.Token()
	if err != nil {
		return err
	}
	value, ok := token.(string)
	if !ok {
		return errors.New("repository field must be a string")
	}
	values[key] = value
	return nil
}

func validateRepositories(repositories []Repository, absolute bool) error {
	for _, repository := range repositories {
		if !validRepositoryPath(repository.Path, absolute) {
			return errors.New("configured repository paths must be nonempty, bounded, and contain no NUL; REPOSITORIES paths must be absolute")
		}
		if len(repository.Name) > 200 || strings.ContainsRune(repository.Name, 0) {
			return errors.New("configured repository names must be no greater than 200 bytes and contain no NUL")
		}
	}
	return nil
}

func validRepositoryPath(path string, absolute bool) bool {
	return strings.TrimSpace(path) != "" && len(path) <= 4096 && !strings.ContainsRune(path, 0) && (!absolute || filepath.IsAbs(path))
}
