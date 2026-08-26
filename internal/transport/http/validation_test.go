package httptransport

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestExplorerRejectsInvalidFiltersBeforeReading(t *testing.T) {
	queries := []string{
		"limit=0", "limit=101", "limit=bad", "offset=-1", "offset=100001", "offset=bad",
		"kind=unknown", "q=" + strings.Repeat("x", 201), "q=%00", "q=%FF",
		"file=../private.go", "file=/private.go", "file=src/../private.go",
	}
	for _, query := range queries {
		handler, catalog, _ := explorerFixture()
		response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols?"+query, "")
		if response.Code != http.StatusBadRequest || catalog.calls != 0 {
			t.Fatalf("invalid query reached catalog: %q %d", query, response.Code)
		}
	}
}

func TestExplorerRejectsInvalidIDsAndFilePaths(t *testing.T) {
	routes := []string{
		"/api/snapshots/not-a-hash/files",
		"/api/snapshots/" + strings.Repeat("A", 64) + "/files",
		"/api/snapshots/" + snapshotID + "/symbols/not-a-hash",
		"/api/snapshots/" + snapshotID + "/file",
		"/api/snapshots/" + snapshotID + "/file?path=../secret",
	}
	for _, route := range routes {
		handler, catalog, _ := explorerFixture()
		response := authorizedRequest(handler, http.MethodGet, route, "")
		if response.Code != http.StatusBadRequest || catalog.calls != 0 {
			t.Fatalf("invalid route reached catalog: %q %d", route, response.Code)
		}
	}
}

func TestExplorerAllowsUnusualCapturedFilenames(t *testing.T) {
	handler, catalog, _ := explorerFixture()
	file := "folder/with spaces\nand-newline.go"
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/file?path="+url.QueryEscape(file), "")
	if response.Code != http.StatusOK || catalog.id != file {
		t.Fatalf("captured valid filename was changed: %d %q", response.Code, catalog.id)
	}
}

func TestExplorerQueryLimitCountsBytes(t *testing.T) {
	for _, count := range []int{100, 101} {
		query := url.Values{"q": []string{strings.Repeat("é", count)}}
		_, err := parseFilter(query, 24)
		if (err != nil) != (count == 101) {
			t.Fatalf("UTF-8 query byte bound incorrect at %d characters", count)
		}
	}
}
