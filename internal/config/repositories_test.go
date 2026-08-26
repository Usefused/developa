package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func repositoryEnvironment(raw string) map[string]string {
	return map[string]string{"DATABASE_URL": "postgres://localhost/db", "REPOSITORIES": raw,
		"DENVERR_API_TOKEN": strings.Repeat("x", 24)}
}

func TestRepositoriesConfigurationPreservesOrderAndLegacyShorthand(t *testing.T) {
	cfg, err := load(environment(repositoryEnvironment(`[{"name":"API","path":"/repos/api"},{"path":"/repos/worker"}]`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 2 || cfg.Repositories[0].Name != "API" || cfg.Repositories[1].Path != "/repos/worker" {
		t.Fatal("configured repository selection changed")
	}
	values := repositoryEnvironment("")
	values["REPOSITORY_PATH"], values["REPOSITORY_NAME"] = "/legacy", "Legacy"
	cfg, err = load(environment(values))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 1 || cfg.Repositories[0].Path != cfg.RepositoryPath || cfg.Repositories[0].Name != cfg.RepositoryName {
		t.Fatal("legacy repository shorthand was not normalized")
	}
}

func TestRepositoriesConfigurationStrictlyRejectsMalformedEntries(t *testing.T) {
	cases := []string{`null`, `{}`, `[null]`, `["/repo"]`, `[{}]`, `[{"path":null}]`, `[{"path":12}]`,
		`[{"path":"/repo","unknown":true}]`, `[{"path":"/repo","path":"/other"}]`, `[{"path":"/repo"}] {}`,
		`[{"name":"x","name":"y","path":"/repo"}]`, `[{"path":"/repo\u0000"}]`, `[{"path":"   "}]`, `[`}
	for _, raw := range cases {
		if _, err := load(environment(repositoryEnvironment(raw))); err == nil {
			t.Fatal("malformed repository configuration accepted")
		}
	}
}

func TestRepositoriesConfigurationBoundsCountBytesAndMetadata(t *testing.T) {
	items := make([]Repository, 32)
	for i := range items {
		items[i] = Repository{Path: "/repo"}
	}
	data, _ := json.Marshal(items)
	if _, err := load(environment(repositoryEnvironment(string(data)))); err != nil {
		t.Fatal(err)
	}
	items = append(items, Repository{Path: "/extra"})
	data, _ = json.Marshal(items)
	assertRepositoriesRejected(t, string(data))
	assertRepositoriesRejected(t, strings.Repeat(" ", 64<<10)+"[]")
	assertRepositoriesRejected(t, "["+string([]byte{255})+"]")
	data, _ = json.Marshal([]Repository{{Name: strings.Repeat("n", 201), Path: "/repo"}})
	assertRepositoriesRejected(t, string(data))
	data, _ = json.Marshal([]Repository{{Path: strings.Repeat("p", 4097)}})
	assertRepositoriesRejected(t, string(data))
}

func assertRepositoriesRejected(t *testing.T, raw string) {
	t.Helper()
	if _, err := load(environment(repositoryEnvironment(raw))); err == nil {
		t.Fatal("unbounded repository configuration accepted")
	}
}

func TestRepositoriesConfigurationRequiresAuthenticationAndExclusiveSource(t *testing.T) {
	values := repositoryEnvironment(`[{"path":"/private/operator-path"}]`)
	values["DENVERR_API_TOKEN"] = "PRIVATE-TOKEN"
	assertSafeRepositoryError(t, values)
	values["DENVERR_API_TOKEN"] = strings.Repeat("x", 24)
	values["REPOSITORY_PATH"] = "/private/legacy"
	assertSafeRepositoryError(t, values)
	delete(values, "REPOSITORY_PATH")
	values["REPOSITORY_NAME"] = "Legacy"
	assertSafeRepositoryError(t, values)
}

func TestRepositoryArrayRequiresAbsolutePathsWhileLegacyKeepsRelativePaths(t *testing.T) {
	assertRepositoriesRejected(t, `[{"path":"relative/repo"}]`)
	values := repositoryEnvironment("")
	values["REPOSITORY_PATH"] = "relative/repo"
	cfg, err := load(environment(values))
	if err != nil || cfg.Repositories[0].Path != "relative/repo" {
		t.Fatal("legacy relative repository shorthand changed")
	}
}

func assertSafeRepositoryError(t *testing.T, values map[string]string) {
	t.Helper()
	_, err := load(environment(values))
	if err == nil {
		t.Fatal("unsafe repository configuration accepted")
	}
	if strings.Contains(err.Error(), "/private/") || strings.Contains(err.Error(), "PRIVATE-TOKEN") {
		t.Fatal("repository configuration error disclosed operator data")
	}
}

func TestEmptyRepositoriesRemainUnconfiguredWithoutToken(t *testing.T) {
	values := repositoryEnvironment("[]")
	delete(values, "DENVERR_API_TOKEN")
	cfg, err := load(environment(values))
	if err != nil || len(cfg.Repositories) != 0 {
		t.Fatal("empty repository selection no longer supports unconfigured mode")
	}
}

func TestWorkspaceRootsRequireBoundedAbsolutePathsAndAuthentication(t *testing.T) {
	values := repositoryEnvironment("")
	values["WORKSPACE_ROOTS"] = `["/repos"]`
	cfg, err := load(environment(values))
	if err != nil || len(cfg.WorkspaceRoots) != 1 {
		t.Fatal("valid workspace root rejected", err)
	}
	for _, raw := range []string{`{}`, `["relative"]`, `[null]`, `["/repos\u0000"]`, `["` + strings.Repeat("x", 4097) + `"]`} {
		values["WORKSPACE_ROOTS"] = raw
		if _, err := load(environment(values)); err == nil {
			t.Fatal("invalid workspace roots accepted")
		}
	}
	values["WORKSPACE_ROOTS"] = `["/repos"]`
	delete(values, "DENVERR_API_TOKEN")
	if _, err := load(environment(values)); err == nil {
		t.Fatal("filesystem access permitted without authentication")
	}
}
