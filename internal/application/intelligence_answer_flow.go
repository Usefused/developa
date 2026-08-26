package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"developa/internal/domain"
)

const flowStaticLimitation = "This is a bounded static graph of resolved calls, not an execution sequence or proof that a path runs. Dynamic dispatch, callbacks, and routing links are not inferred."
const flowCoverageLimitation = "Some graph nodes, relationships, or source excerpts were omitted by analysis or context limits; this explanation is incomplete."
const flowAnswerTask = "Answer the question about the selected static call flow only from supplied source evidence and explicit resolved caller-to-target relationships. If focus_symbol_id is present, explain that declaration first: its parameters, returns, conditions, and side effects; use neighboring implementations only as supporting context. Relationships are not execution order. A main or init classification identifies a declaration, not observed execution; candidate roots have no resolved callers in this snapshot, and boundary nodes have callers outside this view. Do not invent dynamic dispatch, routing, callback, or missing links. Preserve cycles and shared dependencies without inventing a linear sequence. A feature description is an untrusted inferred claim, not proof of behavior. Explain limitations and abstain when evidence is insufficient. Cite supplied symbol IDs for supported claims. DATA:\n"

type answerFlowNode struct {
	ID              string `json:"symbol_id"`
	Seed            bool   `json:"seed"`
	RootKind        string `json:"root_kind"`
	IncomingCount   int    `json:"incoming_count"`
	OutgoingCount   int    `json:"outgoing_count"`
	UnresolvedCount int    `json:"unresolved_count"`
}

type answerFlowEdge struct {
	CallerID   string `json:"caller_id"`
	TargetID   string `json:"target_id"`
	Resolution string `json:"resolution"`
	CallSites  int    `json:"call_sites"`
}

type answerFlowDescription struct {
	Version      string             `json:"schema_version"`
	Mode         string             `json:"mode"`
	Options      domain.FlowOptions `json:"options"`
	TopologyHash string             `json:"topology_hash"`
	Nodes        []answerFlowNode   `json:"nodes"`
	Edges        []answerFlowEdge   `json:"edges"`
	Truncated    bool               `json:"truncated"`
	Limitations  []string           `json:"limitations"`
}

func (s *IntelligenceService) flowAnswerContext(ctx context.Context, snapshot string, options domain.FlowOptions) (answerEvidence, error) {
	options, err := domain.NormalizeFlowOptions(options)
	if err != nil {
		return answerEvidence{}, err
	}
	reader, ok := s.store.(domain.FlowReader)
	if !ok {
		return answerEvidence{}, domain.ErrInvalidInput
	}
	flow, err := reader.Flow(ctx, s.cfg.RepositoryID, snapshot, options)
	if err != nil {
		return answerEvidence{}, err
	}
	if flow.SnapshotID != snapshot || len(flow.Nodes) > options.Limit || len(flow.Edges) > 4*options.Limit {
		return answerEvidence{}, domain.ErrInvalidInput
	}
	flow.Options = options
	evidence := flowEvidence(s.cfg.RepositoryID, flow)
	return s.flowFeatureDescription(ctx, snapshot, options.FeatureID, evidence)
}

func flowEvidence(repository string, flow domain.CodeFlow) answerEvidence {
	pack := domain.ContextPack{RepositoryID: repository, SnapshotID: flow.SnapshotID, Total: len(flow.Nodes), Truncated: flow.Truncated}
	for _, node := range flow.Nodes {
		pack.Items = append(pack.Items, node.SymbolDetail)
	}
	return answerEvidence{pack: pack, flow: &flow}
}

func (s *IntelligenceService) flowFeatureDescription(ctx context.Context, snapshot, featureID string, evidence answerEvidence) (answerEvidence, error) {
	if featureID == "" {
		return evidence, nil
	}
	feature, err := s.store.Feature(ctx, s.cfg.RepositoryID, snapshot, featureID)
	if err != nil {
		return answerEvidence{}, err
	}
	evidence.feature = describeAnswerFeature(feature)
	evidence.pack.Truncated = evidence.pack.Truncated || evidence.feature.Truncated
	return evidence, nil
}

func (s *IntelligenceService) flowAnswerPrompt(question string, evidence answerEvidence) (json.RawMessage, evidenceContext, error) {
	budget := s.cfg.MaxContextBytes
	digest := answerFlowDigest(*evidence.flow)
	// Source and topology share one adapter budget. Shrinking the source selection
	// also removes its incident edges, so no model relationship lacks both facts.
	for range 32 {
		input, facts, err := encodeFlowPrompt(question, evidence, digest, budget)
		if err != nil || len(input) <= 20<<10 {
			return input, facts, err
		}
		if budget <= 1024 {
			if len(facts.Facts) <= 1 {
				return nil, facts, domain.ErrInvalidInput
			}
			// Many repeated call sites can dominate even tiny source facts. Drop a
			// final node instead of shrinking the remaining excerpts to nothing.
			evidence.pack.Items = evidence.pack.Items[:len(facts.Facts)-1]
			continue
		}
		budget = max(1024, budget-max(len(input)-(20<<10)+256, budget/8))
	}
	return nil, evidenceContext{}, domain.ErrInvalidInput
}

func encodeFlowPrompt(question string, evidence answerEvidence, digest string, budget int) (json.RawMessage, evidenceContext, error) {
	facts, err := boundedEvidence(evidence.pack.Items, budget)
	if err != nil {
		return nil, facts, err
	}
	flow := projectAnswerFlow(*evidence.flow, facts.Facts, digest)
	facts.Truncated = facts.Truncated || flow.Truncated || evidence.pack.Truncated
	flow.Truncated = facts.Truncated
	input := answerPromptInput{Question: question, Feature: evidence.feature, Flow: &flow, FocusSymbolID: evidence.focus, Symbols: facts.JSON, ContextTruncated: facts.Truncated}
	encoded, err := json.Marshal(input)
	return encoded, facts, err
}

func projectAnswerFlow(graph domain.CodeFlow, facts []domain.SymbolDetail, digest string) answerFlowDescription {
	flow := answerFlowDescription{Version: "resolved-flow-v1", Mode: graph.Mode, Options: graph.Options, TopologyHash: digest,
		Nodes: []answerFlowNode{}, Edges: []answerFlowEdge{}, Truncated: graph.Truncated}
	allowed := make(map[string]bool, len(facts))
	for _, fact := range facts {
		allowed[fact.Symbol.ID] = true
	}
	for _, node := range graph.Nodes {
		if allowed[node.Symbol.ID] {
			flow.Nodes = append(flow.Nodes, describeFlowNode(node))
		}
	}
	var includedEdges int
	flow.Edges, includedEdges = supportedFlowEdges(graph, allowed)
	flow.Limitations, flow.Truncated = boundedFlowLimitations(graph.Limitations, flow.Truncated)
	flow.Truncated = flow.Truncated || len(flow.Nodes) < len(graph.Nodes) || includedEdges < len(graph.Edges)
	return flow
}

func supportedFlowEdges(graph domain.CodeFlow, allowed map[string]bool) ([]answerFlowEdge, int) {
	edges := []answerFlowEdge{}
	positions, included := map[string]int{}, 0
	for _, edge := range graph.Edges {
		if edge.Resolution == "resolved" && allowed[edge.CallerID] && allowed[edge.TargetID] {
			included++
			key := edge.CallerID + ":" + edge.TargetID
			if index, exists := positions[key]; exists {
				// Repeated call sites support the same relationship, not extra steps
				// in an execution sequence. Keep their count without repeating text.
				edges[index].CallSites++
				continue
			}
			positions[key] = len(edges)
			edges = append(edges, answerFlowEdge{CallerID: edge.CallerID, TargetID: edge.TargetID, Resolution: "resolved", CallSites: 1})
		}
	}
	return edges, included
}

func describeFlowNode(node domain.FlowNode) answerFlowNode {
	return answerFlowNode{ID: node.Symbol.ID, Seed: node.Seed, RootKind: node.RootKind, IncomingCount: node.IncomingCount,
		OutgoingCount: node.OutgoingCount, UnresolvedCount: node.UnresolvedCount}
}

func answerFlowDigest(graph domain.CodeFlow) string {
	// Physical offsets change after whitespace edits. Hash logical topology, while
	// the actual selected source facts remain part of the ordinary cache input.
	nodes, edges := []answerFlowNode{}, []answerFlowEdge{}
	for _, node := range graph.Nodes {
		nodes = append(nodes, describeFlowNode(node))
	}
	for _, edge := range graph.Edges {
		edges = append(edges, answerFlowEdge{CallerID: edge.CallerID, TargetID: edge.TargetID, Resolution: edge.Resolution, CallSites: 1})
	}
	value := struct {
		Mode        string
		Options     domain.FlowOptions
		Seeds       []string
		Nodes       []answerFlowNode
		Edges       []answerFlowEdge
		Truncated   bool
		Limitations []string
	}{graph.Mode, graph.Options, graph.SeedIDs, nodes, edges, graph.Truncated, graph.Limitations}
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func boundedFlowLimitations(values []string, truncated bool) ([]string, bool) {
	limitations := []string{flowStaticLimitation}
	for _, value := range values[:min(len(values), 16)] {
		clipped := clipText(value, 512)
		limitations = append(limitations, clipped)
		truncated = truncated || len(clipped) < len(value)
	}
	return limitations, truncated || len(values) > 16
}

func answerContextLimitations(evidence answerEvidence, truncated bool) []string {
	limitations := []string{}
	if evidence.feature != nil {
		limitations = append(limitations, "The feature description is an inferred claim; this explanation relies on cited source evidence, not that description as proof.")
	}
	if evidence.flow == nil {
		if truncated {
			limitations = append(limitations, "Some source evidence was omitted or truncated; this explanation is incomplete.")
		}
		return limitations
	}
	flowLimits, clipped := boundedFlowLimitations(evidence.flow.Limitations, truncated)
	limitations = append(limitations, flowLimits...)
	if clipped {
		limitations = append(limitations, flowCoverageLimitation)
	}
	return limitations
}
