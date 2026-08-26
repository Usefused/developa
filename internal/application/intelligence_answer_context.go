package application

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"

	"developa/internal/domain"
)

type answerEvidence struct {
	pack    domain.ContextPack
	feature *answerFeatureDescription
	flow    *domain.CodeFlow
	focus   string
}

type answerFeatureDescription struct {
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	Truncated bool   `json:"truncated"`
}

type answerPromptInput struct {
	Question         string                    `json:"question"`
	Feature          *answerFeatureDescription `json:"feature,omitempty"`
	Flow             *answerFlowDescription    `json:"flow,omitempty"`
	FocusSymbolID    string                    `json:"focus_symbol_id,omitempty"`
	Symbols          json.RawMessage           `json:"symbols"`
	ContextTruncated bool                      `json:"context_truncated"`
}

func validAnswerRequest(request domain.AnswerRequest) bool {
	if !boundedText(request.Question, 2000) || request.SymbolID != "" && request.FeatureID != "" {
		return false
	}
	if request.Flow != nil {
		return validFlowAnswerTarget(request)
	}
	return validAnswerReference(request.SymbolID) && validAnswerReference(request.FeatureID)
}

func validFlowAnswerTarget(request domain.AnswerRequest) bool {
	if request.SymbolID != "" || request.FeatureID != "" {
		return false
	}
	_, err := domain.NormalizeFlowOptions(*request.Flow)
	return err == nil
}

func validAnswerReference(id string) bool {
	if id == "" {
		return true
	}
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && id == strings.ToLower(id)
}

func (s *IntelligenceService) answerContext(ctx context.Context, snapshotID string, request domain.AnswerRequest) (answerEvidence, error) {
	if request.Flow != nil {
		return s.flowAnswerContext(ctx, snapshotID, *request.Flow)
	}
	if request.FeatureID != "" {
		return s.featureAnswerContext(ctx, snapshotID, request.FeatureID)
	}
	if request.SymbolID == "" {
		pack, err := s.store.Context(ctx, s.cfg.RepositoryID, snapshotID, request.Question, s.cfg.BatchSize)
		return answerEvidence{pack: pack}, err
	}
	symbol, err := s.store.Symbol(ctx, s.cfg.RepositoryID, snapshotID, request.SymbolID)
	if err != nil {
		return answerEvidence{}, err
	}
	if _, ok := s.store.(domain.FlowReader); ok {
		return s.symbolAnswerContext(ctx, snapshotID, symbol)
	}
	pack := domain.ContextPack{RepositoryID: s.cfg.RepositoryID, SnapshotID: snapshotID, Items: []domain.SymbolDetail{symbol}, Total: 1}
	return answerEvidence{pack: pack}, nil
}

func (s *IntelligenceService) symbolAnswerContext(ctx context.Context, snapshot string, symbol domain.SymbolDetail) (answerEvidence, error) {
	// The seed is returned first by the graph query, keeping the selected
	// implementation in the prompt even when neighboring evidence hits a limit.
	evidence, err := s.flowAnswerContext(ctx, snapshot, domain.FlowOptions{SymbolID: symbol.Symbol.ID, Depth: 1, Limit: 9})
	if err != nil {
		return answerEvidence{}, err
	}
	if len(evidence.pack.Items) == 0 || evidence.pack.Items[0].Symbol.ID != symbol.Symbol.ID {
		return answerEvidence{}, domain.ErrInvalidInput
	}
	evidence.focus = symbol.Symbol.ID
	return evidence, nil
}

func (s *IntelligenceService) featureAnswerContext(ctx context.Context, snapshotID, featureID string) (answerEvidence, error) {
	feature, err := s.store.Feature(ctx, s.cfg.RepositoryID, snapshotID, featureID)
	if err != nil {
		return answerEvidence{}, err
	}
	reader, ok := s.store.(domain.FeatureContextReader)
	if !ok {
		return answerEvidence{}, domain.ErrInvalidInput
	}
	pack, err := reader.FeatureContext(ctx, s.cfg.RepositoryID, snapshotID, featureID, s.cfg.BatchSize)
	if err != nil {
		return answerEvidence{}, err
	}
	description := describeAnswerFeature(feature)
	pack.Truncated = pack.Truncated || description.Truncated
	return answerEvidence{pack: pack, feature: description}, nil
}

func describeAnswerFeature(feature domain.Feature) *answerFeatureDescription {
	description := &answerFeatureDescription{Title: clipText(feature.Title, 160), Summary: clipText(feature.Summary, 2000), Status: "inferred"}
	description.Truncated = len(description.Title) < len(feature.Title) || len(description.Summary) < len(feature.Summary)
	return description
}

func (s *IntelligenceService) answerPrompt(question string, evidence answerEvidence) (json.RawMessage, evidenceContext, error) {
	if len(evidence.pack.Items) == 0 {
		return nil, evidenceContext{}, nil
	}
	if evidence.flow != nil {
		return s.flowAnswerPrompt(question, evidence)
	}
	input := answerPromptInput{Question: question, Feature: evidence.feature, Symbols: json.RawMessage(`[]`)}
	metadata, _ := json.Marshal(input)
	// Reserve adapter/schema overhead and account for JSON escaping before choosing source excerpts.
	budget := min(s.cfg.MaxContextBytes, (20<<10)-len(metadata)+2)
	if budget < 1024 {
		return nil, evidenceContext{}, domain.ErrInvalidInput
	}
	facts, err := boundedEvidence(evidence.pack.Items, budget)
	if err != nil {
		return nil, facts, err
	}
	input.Symbols = facts.JSON
	input.ContextTruncated = evidence.pack.Truncated || facts.Truncated || len(facts.Facts) < evidence.pack.Total
	encoded, err := json.Marshal(input)
	return encoded, facts, err
}
