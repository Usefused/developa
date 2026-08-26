package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

type flowAnswerTestStore struct {
	*featureAnswerTestStore
	graph                        domain.CodeFlow
	flowReads                    int
	flowRepository, flowSnapshot string
	flowOptions                  domain.FlowOptions
	flowErr                      error
}

func (s *flowAnswerTestStore) Flow(ctx context.Context, repository, snapshot string, options domain.FlowOptions) (domain.CodeFlow, error) {
	s.flowReads++
	s.flowRepository, s.flowSnapshot, s.flowOptions = repository, snapshot, options
	if err := ctx.Err(); err != nil {
		return domain.CodeFlow{}, err
	}
	graph := s.graph
	graph.SnapshotID, graph.Options = snapshot, options
	return graph, s.flowErr
}

func flowAnswerFixture(t *testing.T, count int, cfg IntelligenceConfig) (*IntelligenceService, *flowAnswerTestStore, *resolvingTestModel) {
	t.Helper()
	_, base, model := featureAnswerFixture(t, count, cfg)
	store := &flowAnswerTestStore{featureAnswerTestStore: base, graph: answerTestFlow(base.symbols)}
	return cacheService(t, store, model, cfg), store, model
}

func answerTestFlow(symbols []domain.SymbolDetail) domain.CodeFlow {
	graph := domain.CodeFlow{Mode: "application", Nodes: []domain.FlowNode{}, Edges: []goparser.Call{}}
	for index, symbol := range symbols {
		node := domain.FlowNode{SymbolDetail: symbol, Seed: index == 0}
		if index == 0 {
			node.RootKind = "candidate"
			graph.SeedIDs = []string{symbol.Symbol.ID}
		}
		if index > 0 {
			node.IncomingCount = 1
			graph.Nodes[index-1].OutgoingCount = 1
			graph.Edges = append(graph.Edges, goparser.Call{CallerID: symbols[index-1].Symbol.ID, TargetID: symbol.Symbol.ID, Resolution: "resolved"})
		}
		graph.Nodes = append(graph.Nodes, node)
	}
	return graph
}

func captureFlowPrompt(t *testing.T, model *resolvingTestModel) *answerPromptInput {
	t.Helper()
	input := &answerPromptInput{}
	generate := model.generate
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		if system != groundingInstructions || !strings.Contains(prompt, "not execution order") {
			t.Fatal("flow instructions did not distinguish static relationships from execution")
		}
		if len(system)+len(prompt)+len(schema) > 24<<10 {
			t.Fatal("flow prompt exceeded the adapter budget")
		}
		if err := json.Unmarshal([]byte(strings.SplitN(prompt, "DATA:\n", 2)[1]), input); err != nil {
			t.Fatal(err)
		}
		if input.Flow == nil {
			t.Fatal("flow metadata absent from explicit explanation")
		}
		return generate(ctx, system, prompt, schema)
	}
	return input
}

func TestFlowAnswerReadsOneScopedGraphAndPreservesClassifications(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 3, IntelligenceConfig{})
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain this flow", Flow: &domain.FlowOptions{}})
	if store.flowReads != 1 || store.flowRepository != "repository" || store.flowSnapshot != "snapshot" {
		t.Fatal("flow explanation lost its bounded repository/snapshot scope")
	}
	if store.flowOptions != (domain.FlowOptions{Depth: 6, Limit: 80}) || store.symbolReads != 0 || store.contextReads != 0 {
		t.Fatal("flow explanation did not use normalized set-based retrieval")
	}
	assertFlowPromptClassifications(t, input.Flow, store.graph)
	if answer.ContextTruncated || !slices.Contains(answer.Limitations, flowStaticLimitation) {
		t.Fatal("complete static evidence was incorrectly labeled or lacked the runtime limitation")
	}
}

func TestSymbolExplanationIncludesBoundedNeighborsAndExplicitFocus(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 3, IntelligenceConfig{})
	id := store.symbols[0].Symbol.ID
	store.graph.Mode = "symbol"
	store.graph.Nodes[0].Symbol.Documentation = &goparser.Documentation{Summary: "Run validates input.\n\nReject empty values."}
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "pinned", domain.AnswerRequest{Question: "Explain this function", SymbolID: id})
	if store.symbolReads != 1 || store.flowReads != 1 || store.contextReads != 0 || store.flowSnapshot != "pinned" {
		t.Fatal("function context did not use bounded, snapshot-pinned reads")
	}
	if store.flowOptions != (domain.FlowOptions{SymbolID: id, Depth: 1, Limit: 9}) || input.FocusSymbolID != id {
		t.Fatal("selected function lost priority or graph bounds")
	}
	assertSymbolExplanationEvidence(t, input, answer)
}

func assertSymbolExplanationEvidence(t *testing.T, input *answerPromptInput, answer domain.Answer) {
	t.Helper()
	var symbols []promptSymbol
	if err := json.Unmarshal(input.Symbols, &symbols); err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 3 || !strings.Contains(symbols[0].Doc, "Reject empty values.") || len(input.Flow.Edges) != 2 || answer.ContextTruncated {
		t.Fatal("implementation, compiled comments, or relationships missing")
	}
}

func TestSymbolExplanationBudgetRetainsFocusAndDisclosesMissingContext(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 3, IntelligenceConfig{MaxContextBytes: 1024})
	id := store.symbols[0].Symbol.ID
	store.graph.Nodes[0].Symbol.Source = strings.Repeat("x", 8192)
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", SymbolID: id})
	if input.FocusSymbolID != id || input.Flow.Nodes[0].ID != id || !answer.ContextTruncated {
		t.Fatal("context budgeting lost the focus or hid omitted evidence")
	}
	var symbols []promptSymbol
	if err := json.Unmarshal(input.Symbols, &symbols); err != nil {
		t.Fatal(err)
	}
	if !symbols[0].Truncated || len(symbols[0].Source) >= len(store.graph.Nodes[0].Symbol.Source) {
		t.Fatal("oversized source was not bounded or marked incomplete")
	}
}

func TestSymbolExplanationCacheIncludesNeighborEvidence(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 2, IntelligenceConfig{})
	request := domain.AnswerRequest{Question: "Explain", SymbolID: store.symbols[0].Symbol.ID}
	cachedAnswer(t, service, "snapshot", request)
	reused := cachedAnswer(t, service, "snapshot", request)
	store.graph.Nodes[1].Symbol.Source = "func Changed() { return }"
	changed := cachedAnswer(t, service, "snapshot", request)
	if !reused.Cached || changed.Cached || model.calls.Load() != 2 {
		t.Fatal("neighbor changes did not invalidate context or unchanged evidence spent tokens")
	}
}

func assertFlowPromptClassifications(t *testing.T, prompt *answerFlowDescription, graph domain.CodeFlow) {
	t.Helper()
	if len(prompt.Nodes) != len(graph.Nodes) || len(prompt.Edges) != len(graph.Edges) || prompt.Version != "resolved-flow-v1" {
		t.Fatal("flow projection lost nodes, relationships, or schema scope")
	}
	if !prompt.Nodes[0].Seed || prompt.Nodes[0].RootKind != "candidate" || prompt.Nodes[1].IncomingCount != 1 {
		t.Fatal("seed/root classifications were recomputed from the model slice")
	}
	if prompt.Edges[0].CallerID != graph.Edges[0].CallerID || prompt.Edges[0].TargetID != graph.Edges[0].TargetID {
		t.Fatal("caller-to-target relationship reversed or invented")
	}
}

func TestFlowAnswerPreservesCyclesWithoutInventingAnEntryPoint(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 3, IntelligenceConfig{})
	store.graph.Nodes[0].RootKind = ""
	store.graph.Nodes[0].IncomingCount = 1
	store.graph.Nodes[2].OutgoingCount = 1
	store.graph.Edges = append(store.graph.Edges, goparser.Call{CallerID: store.symbols[2].Symbol.ID, TargetID: store.symbols[0].Symbol.ID, Resolution: "resolved"})
	store.graph.Limitations = []string{"No resolved root exists in this cyclic component."}
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain this cycle", Flow: &domain.FlowOptions{}})
	if len(input.Flow.Nodes) != 3 || len(input.Flow.Edges) != 3 || input.Flow.Nodes[0].RootKind != "" {
		t.Fatal("cycle was expanded repeatedly, cut, or reclassified as an entrypoint")
	}
	if !slices.Contains(answer.Limitations, store.graph.Limitations[0]) {
		t.Fatal("no-root limitation was omitted")
	}
}

func TestFlowAnswerOmitsUnresolvedAndUncitedEdges(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 2, IntelligenceConfig{})
	store.graph.Edges = append(store.graph.Edges,
		goparser.Call{CallerID: store.symbols[0].Symbol.ID, Resolution: "unresolved"},
		goparser.Call{CallerID: store.symbols[0].Symbol.ID, TargetID: strings.Repeat("f", 64), Resolution: "resolved"})
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if len(input.Flow.Edges) != 1 || !input.Flow.Truncated || !answer.ContextTruncated {
		t.Fatal("unsupported relationship leaked or omitted evidence was hidden")
	}
	if !slices.Contains(answer.Limitations, flowCoverageLimitation) {
		t.Fatal("projection incompleteness was not published")
	}
}

func TestFlowAnswerSourceBudgetRemovesIncidentEdges(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 20, IntelligenceConfig{MaxContextBytes: 2048})
	for index := range store.graph.Nodes {
		store.graph.Nodes[index].Symbol.Source = strings.Repeat("x", 8192)
	}
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if len(input.Flow.Nodes) != 1 || len(input.Flow.Edges) != 0 || !answer.ContextTruncated {
		t.Fatal("source budget retained relationships without both endpoint facts")
	}
	if input.Flow.Nodes[0].OutgoingCount != 1 || !input.Flow.Truncated {
		t.Fatal("full-snapshot counts were replaced by partial model counts")
	}
}

func TestFlowAnswerPromptBoundsDenseTopologyAndEscapedQuestion(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 100, IntelligenceConfig{})
	for index := 0; len(store.graph.Edges) < 400; index++ {
		store.graph.Edges = append(store.graph.Edges, goparser.Call{CallerID: store.symbols[index%10].Symbol.ID, TargetID: store.symbols[(index+1)%10].Symbol.ID, Resolution: "resolved"})
	}
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: strings.Repeat("\"", 2000), Flow: &domain.FlowOptions{Depth: 12, Limit: 100}})
	if !answer.ContextTruncated || len(input.Flow.Nodes) >= 100 {
		t.Fatal("dense graph did not share the bounded model budget with source evidence")
	}
	assertFlowEdgesHaveSource(t, input)
}

func TestFlowAnswerCompactsRepeatedCallSitesWithoutLosingCoverage(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 1, IntelligenceConfig{})
	for range 320 {
		store.graph.Edges = append(store.graph.Edges, goparser.Call{CallerID: store.symbols[0].Symbol.ID, TargetID: store.symbols[0].Symbol.ID, Resolution: "resolved"})
	}
	store.graph.Nodes[0].RootKind = ""
	store.graph.Nodes[0].IncomingCount, store.graph.Nodes[0].OutgoingCount = 320, 320
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if len(input.Flow.Edges) != 1 || input.Flow.Edges[0].CallSites != 320 || answer.ContextTruncated {
		t.Fatal("repeated static call sites overflowed context or were falsely marked omitted")
	}
}

func assertFlowEdgesHaveSource(t *testing.T, input *answerPromptInput) {
	t.Helper()
	var symbols []promptSymbol
	if err := json.Unmarshal(input.Symbols, &symbols); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, symbol := range symbols {
		allowed[symbol.ID] = true
	}
	for _, edge := range input.Flow.Edges {
		if !allowed[edge.CallerID] || !allowed[edge.TargetID] || edge.Resolution != "resolved" {
			t.Fatal("projected relationship lacks two source endpoints or static resolution")
		}
	}
}

func TestFlowAnswerCacheTracksTopologyOptionsAndCanonicalFacts(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 3, IntelligenceConfig{})
	request := domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}}
	first := cachedAnswer(t, service, "first", request)
	store.graph.Nodes[0].Symbol.Span.Start.Line = 40
	request.Flow = &domain.FlowOptions{Depth: 6, Limit: 80}
	second := cachedAnswer(t, service, "second", request)
	if first.Cached || !second.Cached || second.Evidence[0].Span.Start.Line != 40 || model.calls.Load() != 1 {
		t.Fatal("normalized options or shifted positions lost cache reuse/canonical citations")
	}
	store.graph.Edges[1].TargetID = store.graph.Nodes[0].Symbol.ID
	cachedAnswer(t, service, "second", request)
	request.Flow.Depth = 5
	cachedAnswer(t, service, "second", request)
	store.graph.Nodes[0].RootKind = "boundary"
	cachedAnswer(t, service, "second", request)
	store.graph.Nodes[0].Symbol.Source = "func Run() { changed() }"
	cachedAnswer(t, service, "second", request)
	if model.calls.Load() != 5 {
		t.Fatal("changed topology, options, root classification, or source reused stale explanation")
	}
}

func TestFlowAnswerCacheTracksTopologyOutsideSourceProjection(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 20, IntelligenceConfig{MaxContextBytes: 2048})
	request := domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}}
	first := cachedAnswer(t, service, "snapshot", request)
	store.graph.Edges[18].TargetID = store.graph.Nodes[0].Symbol.ID
	second := cachedAnswer(t, service, "snapshot", request)
	if !first.ContextTruncated || second.Cached || model.calls.Load() != 2 {
		t.Fatal("changed bounded graph outside supplied source reused stale topology")
	}
}

func TestFlowAnswerDoesNotShareGenericOrSymbolCacheEntries(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 1, IntelligenceConfig{})
	cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain"})
	cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", SymbolID: store.symbols[0].Symbol.ID})
	flow := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if flow.Cached || model.calls.Load() != 3 {
		t.Fatal("flow explanation reused a generic/symbol explanation cache")
	}
}

func TestFlowAnswerWithoutSourceDoesNotInvokeModel(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 0, IntelligenceConfig{})
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if !answer.InsufficientEvidence || answer.Text != insufficientAnswer || model.calls.Load() != 0 || model.resolves != 0 {
		t.Fatal("empty static graph triggered model inference")
	}
	if store.answer.ID != answer.ID || !slices.Contains(answer.Limitations, flowStaticLimitation) {
		t.Fatal("empty graph abstention was not audited with its static limitation")
	}
}

func TestFlowAnswerMissingScopeAndCancellationDoNotInvokeModel(t *testing.T) {
	for _, failure := range []error{domain.ErrNotFound, context.Canceled} {
		service, store, model := flowAnswerFixture(t, 1, IntelligenceConfig{})
		store.flowErr = failure
		_, err := service.Answer(context.Background(), "missing-snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{SymbolID: store.symbols[0].Symbol.ID}})
		if !errors.Is(err, failure) || model.calls.Load() != 0 || store.answer.ID != "" {
			t.Fatal("failed or canceled flow lookup invoked inference or published an answer")
		}
	}
}

func TestFlowAnswerRequiresOptionalFlowReader(t *testing.T) {
	service, _, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	_, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if !errors.Is(err, domain.ErrInvalidInput) || model.calls.Load() != 0 {
		t.Fatal("flow answer silently fell back to generic context retrieval")
	}
}

func TestFlowAnswerPropagatesRequestCancellationToGraphRead(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 1, IntelligenceConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Answer(ctx, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{}})
	if !errors.Is(err, context.Canceled) || model.calls.Load() != 0 || store.answer.ID != "" {
		t.Fatal("canceled request proceeded to inference or publication")
	}
}

func TestFlowAnswerRejectsStoreResponsesBeyondRequestedBound(t *testing.T) {
	service, _, model := flowAnswerFixture(t, 2, IntelligenceConfig{})
	_, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{Limit: 1}})
	if !errors.Is(err, domain.ErrInvalidInput) || model.calls.Load() != 0 {
		t.Fatal("out-of-contract graph response reached inference")
	}
}

func TestFeatureFlowUsesOneMetadataReadAndMarksClaimInferred(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 3, IntelligenceConfig{})
	store.graph.Mode = "feature"
	input := captureFlowPrompt(t, model)
	answer := cachedAnswer(t, service, "snapshot", domain.AnswerRequest{Question: "Explain", Flow: &domain.FlowOptions{FeatureID: store.feature.ID}})
	if store.flowReads != 1 || store.featureReads != 1 || store.featureContextReads != 0 || store.symbolReads != 0 {
		t.Fatal("feature flow did not use a fixed scoped query budget")
	}
	if input.Feature == nil || input.Feature.Status != "inferred" || input.Feature.Summary != store.feature.Summary {
		t.Fatal("feature metadata was not separated from proof")
	}
	if len(answer.Limitations) < 2 || input.Flow.Options.FeatureID != store.feature.ID {
		t.Fatal("feature selector or limitations disappeared")
	}
}

func TestFlowAnswerInvalidSelectorsRejectedBeforeRetrieval(t *testing.T) {
	service, store, model := flowAnswerFixture(t, 1, IntelligenceConfig{})
	id := store.symbols[0].Symbol.ID
	requests := []domain.AnswerRequest{
		{Question: "Explain", SymbolID: id, Flow: &domain.FlowOptions{}},
		{Question: "Explain", FeatureID: id, Flow: &domain.FlowOptions{}},
		{Question: "Explain", Flow: &domain.FlowOptions{SymbolID: id, FeatureID: id}},
		{Question: "Explain", Flow: &domain.FlowOptions{SymbolID: "bad"}},
		{Question: "Explain", Flow: &domain.FlowOptions{Depth: 13}},
		{Question: "Explain", Flow: &domain.FlowOptions{Limit: 101}},
	}
	for _, request := range requests {
		if _, err := service.Answer(context.Background(), "snapshot", request); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatal("ambiguous or unbounded flow selection accepted")
		}
	}
	if store.flowReads != 0 || model.calls.Load() != 0 {
		t.Fatal("invalid flow selection reached storage or inference")
	}
}
