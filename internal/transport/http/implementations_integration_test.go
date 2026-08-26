package httptransport

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

const implementationFixtureSource = `package fixture
type Runner interface { Run() int; Reset() }
type Engine struct{}
func (Engine) Run() int { return 1 }
func (Engine) Reset() {}
func Invoke(r Runner) int { return r.Run() }
`

func TestIntegrationImplementationCandidatesFromGitThroughPostgresAndHTTP(t *testing.T) {
	fixture := newIntegrationExplorer(t)
	integrationWrite(t, fixture.root, "main.go", implementationFixtureSource)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	base := repositoryPrefix(fixture) + "/snapshots/" + snapshot.ID
	selected := implementationIntegrationInterface(t, fixture, base)
	var page domain.ImplementationPage
	integrationRead(t, fixture, base+"/symbols/"+selected+"/implementations?limit=1", &page)
	assertImplementationPage(t, page, fixture.manager.Repository().ID, snapshot.ID, selected)
	candidate := page.Items[0]
	assertImplementationReferences(t, candidate, selected)
	assertImplementationMethodNavigation(t, fixture, base, candidate)
	assertImplementationPagination(t, fixture, base, selected, candidate)
	assertImplementationCallReferences(t, fixture, base, selected)
	assertImplementationEndpointBoundaries(t, fixture, base, selected)
}

func implementationIntegrationInterface(t *testing.T, fixture *integrationExplorer, base string) string {
	t.Helper()
	var page domain.SymbolPage
	integrationRead(t, fixture, base+"/symbols?q=Runner&kind=interface", &page)
	if len(page.Items) != 1 {
		t.Fatalf("expected one named interface, got %+v", page)
	}
	return page.Items[0].Symbol.ID
}

func assertImplementationPage(t *testing.T, page domain.ImplementationPage, repository, snapshot, symbol string) {
	t.Helper()
	if page.RepositoryID != repository || page.SnapshotID != snapshot || page.SymbolID != symbol {
		t.Fatalf("implementation page lost scope: %+v", page)
	}
	if page.Total != 2 || page.Limit != 1 || len(page.Items) != 1 {
		t.Fatalf("interface method links did not paginate: %+v", page)
	}
	if page.Analysis.Status != "complete" || len(page.Analysis.Limitations) == 0 {
		t.Fatal("implementation page omitted static-analysis coverage")
	}
}

func assertImplementationReferences(t *testing.T, candidate goparser.Implementation, selected string) {
	t.Helper()
	if candidate.Interface.SymbolID != selected || candidate.Receiver.Name != "Engine" || candidate.Pointer {
		t.Fatalf("candidate identity or value receiver was lost: %+v", candidate)
	}
	if candidate.Evidence != "go_types_method_set" || candidate.Method.Name != candidate.Target.Name {
		t.Fatalf("candidate lost its static method-set evidence: %+v", candidate)
	}
	for _, reference := range []goparser.SymbolReference{candidate.Interface, candidate.Method, candidate.Receiver, candidate.Target} {
		if !validID(reference.SymbolID) || reference.Path != "main.go" || reference.Span.Start.Line < 1 {
			t.Fatalf("candidate is missing a source link: %+v", reference)
		}
	}
}

func assertImplementationMethodNavigation(t *testing.T, fixture *integrationExplorer, base string, candidate goparser.Implementation) {
	t.Helper()
	var methodPage domain.ImplementationPage
	integrationRead(t, fixture, base+"/symbols/"+candidate.Method.SymbolID+"/implementations", &methodPage)
	if methodPage.Total != 1 || len(methodPage.Items) != 1 || methodPage.Items[0].Target.SymbolID != candidate.Target.SymbolID {
		t.Fatal("method selector did not preserve the candidate target")
	}
	var detail domain.SymbolDetail
	integrationRead(t, fixture, base+"/symbols/"+candidate.Target.SymbolID, &detail)
	if detail.Path != candidate.Target.Path || detail.Symbol.Span != candidate.Target.Span {
		t.Fatal("candidate target could not be followed to its declaration")
	}
	var source domain.SymbolSource
	integrationRead(t, fixture, base+"/symbols/"+candidate.Target.SymbolID+"/source", &source)
	if source.SymbolID != candidate.Target.SymbolID || !strings.Contains(source.Source, candidate.Target.Name) {
		t.Fatal("candidate target could not be followed to retained source")
	}
}

func assertImplementationPagination(t *testing.T, fixture *integrationExplorer, base, selected string, previous goparser.Implementation) {
	t.Helper()
	var next domain.ImplementationPage
	integrationRead(t, fixture, base+"/symbols/"+selected+"/implementations?limit=1&offset=1", &next)
	if next.Total != 2 || next.Offset != 1 || len(next.Items) != 1 {
		t.Fatal("implementation page lost pagination metadata")
	}
	if next.Items[0].Method.SymbolID == previous.Method.SymbolID {
		t.Fatal("implementation offset repeated the previous method")
	}
	var empty domain.ImplementationPage
	integrationRead(t, fixture, base+"/symbols/"+selected+"/implementations?offset=100000", &empty)
	if len(empty.Items) != 0 || empty.Total != 2 || empty.Analysis.Status != next.Analysis.Status {
		t.Fatal("empty implementation page lost total or analysis status")
	}
}

func assertImplementationCallReferences(t *testing.T, fixture *integrationExplorer, base, selected string) {
	t.Helper()
	var calls domain.CallPage
	integrationRead(t, fixture, base+"/calls?resolution=unresolved", &calls)
	if len(calls.Items) != 1 {
		t.Fatalf("expected one unresolved interface call, got %+v", calls)
	}
	call := calls.Items[0]
	if call.TargetID != "" || call.Target != nil || call.Interface == nil || call.InterfaceMethod == nil {
		t.Fatal("interface call became resolved or lost interface source references")
	}
	if call.Interface.SymbolID != selected || call.InterfaceMethod.Name != "Run" {
		t.Fatal("interface call references do not identify its declarations")
	}
}

func assertImplementationEndpointBoundaries(t *testing.T, fixture *integrationExplorer, base, selected string) {
	t.Helper()
	endpoint := base + "/symbols/" + selected + "/implementations"
	if status := integrationRequest(t, fixture, http.MethodGet, endpoint, false, nil); status != http.StatusUnauthorized {
		t.Fatal("implementation endpoint permitted anonymous source access")
	}
	if status := integrationRequest(t, fixture, http.MethodGet, endpoint+"?limit=101", true, nil); status != http.StatusBadRequest {
		t.Fatal("implementation endpoint permitted an unbounded page")
	}
	missing := base + "/symbols/" + strings.Repeat("f", 64) + "/implementations"
	if status := integrationRequest(t, fixture, http.MethodGet, missing, true, nil); status != http.StatusNotFound {
		t.Fatal("implementation endpoint accepted a missing symbol")
	}
}

func TestIntegrationImplementationCandidatesRejectForeignRepositorySnapshots(t *testing.T) {
	group, fixtures := integrationRepositories(t)
	integrationWrite(t, fixtures[0].root, "main.go", implementationFixtureSource)
	group.Start(context.Background())
	first := awaitRepositorySnapshot(t, fixtures[0], "")
	second := awaitRepositorySnapshot(t, fixtures[1], "")
	selected := implementationIntegrationInterface(t, fixtures[0], repositoryPrefix(fixtures[0])+"/snapshots/"+first.ID)
	for _, snapshot := range []string{first.ID, second.ID} {
		path := repositoryPrefix(fixtures[1]) + "/snapshots/" + snapshot + "/symbols/" + selected + "/implementations"
		if status := integrationRequest(t, fixtures[1], http.MethodGet, path, true, nil); status != http.StatusNotFound {
			t.Fatal("implementation candidates crossed a repository or snapshot boundary")
		}
	}
}
