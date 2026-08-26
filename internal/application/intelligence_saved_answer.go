package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"developa/internal/domain"
	"go.opentelemetry.io/otel/attribute"
)

type preparedAnswer struct {
	evidence answerEvidence
	input    json.RawMessage
	facts    evidenceContext
	key      string
}

func (s *IntelligenceService) prepareAnswer(ctx context.Context, snapshot string, request domain.AnswerRequest) (preparedAnswer, error) {
	evidence, err := s.answerContext(ctx, snapshot, request)
	if err != nil {
		return preparedAnswer{}, err
	}
	input, facts, err := s.answerPrompt(request.Question, evidence)
	if err != nil {
		return preparedAnswer{}, err
	}
	return preparedAnswer{evidence: evidence, input: input, facts: facts, key: savedAnswerKey(request, evidence, input)}, nil
}

func savedAnswerKey(request domain.AnswerRequest, evidence answerEvidence, input json.RawMessage) string {
	if request.Flow != nil {
		normalized, _ := domain.NormalizeFlowOptions(*request.Flow)
		request.Flow = &normalized
	}
	// Saved documents outlive model availability. The complete function content
	// hashes and bounded supporting facts live in input; physical lines do not.
	data, _ := json.Marshal([]any{"saved-answer-v1", request, string(input), evidence.pack.Total, evidence.pack.Truncated,
		groundingInstructions, answerTask, flowAnswerTask, string(answerSchema)})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (s *IntelligenceService) SavedAnswer(ctx context.Context, snapshot string, request domain.AnswerRequest) (*domain.Answer, error) {
	if !validAnswerRequest(request) {
		return nil, domain.ErrInvalidInput
	}
	ctx, span := scanTracer().Start(ctx, "intelligence.saved_answer")
	defer span.End()
	span.SetAttributes(attribute.String("repository.id", s.cfg.RepositoryID))
	prepared, err := s.prepareAnswer(ctx, snapshot, request)
	if err != nil {
		return nil, err
	}
	return s.readSavedAnswer(ctx, snapshot, prepared)
}

func (s *IntelligenceService) readSavedAnswer(ctx context.Context, snapshot string, prepared preparedAnswer) (*domain.Answer, error) {
	store, ok := s.store.(domain.SavedAnswerStore)
	if !ok {
		return nil, nil
	}
	answer, err := store.SavedAnswer(ctx, s.cfg.RepositoryID, snapshot, prepared.key)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := validateSavedAnswer(&answer, prepared.facts.Facts); err != nil {
		return nil, err
	}
	return &answer, nil
}

func validateSavedAnswer(answer *domain.Answer, facts []domain.SymbolDetail) error {
	ids := make([]string, len(answer.Evidence))
	for i, citation := range answer.Evidence {
		ids[i] = citation.SymbolID
	}
	evidence, err := canonicalEvidence(ids, facts, !answer.InsufficientEvidence)
	if err != nil {
		return err
	}
	if !boundedText(answer.Text, 16000) {
		return domain.ErrInvalidModelOutput
	}
	answer.Evidence, answer.Cached = evidence, true
	answer.Limitations = append([]string{"Saved explanation for unchanged question and code evidence; no model was contacted."}, answer.Limitations...)
	return nil
}
