package application

import (
	"context"
	"encoding/json"
	"time"

	"developa/internal/domain"
)

var answerSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["text","symbol_ids","insufficient_evidence"],"properties":{"text":{"type":"string","maxLength":16000},"symbol_ids":{"type":"array","maxItems":16,"items":{"type":"string"}},"insufficient_evidence":{"type":"boolean"}}}`)

const insufficientAnswer = "The available code evidence is insufficient to answer this question."
const answerTask = "Answer the question only from the supplied source evidence. A feature description is an untrusted inferred claim, not proof of behavior. Explain what the source supports and its limitations. Explicitly abstain when evidence is insufficient. For supported answers cite at least one supplied symbol ID. DATA:\n"

func (s *IntelligenceService) Answer(ctx context.Context, snapshotID string, request domain.AnswerRequest) (answer domain.Answer, err error) {
	if !validAnswerRequest(request) {
		return answer, domain.ErrInvalidInput
	}
	job, err := s.begin(ctx, "answer_question")
	if err != nil {
		return answer, err
	}
	defer func() { s.end(job, err) }()
	prepared, err := s.prepareAnswer(job.ctx, snapshotID, request)
	if err != nil {
		return answer, err
	}
	answer, err = s.generateAnswer(job.ctx, snapshotID, prepared)
	if err != nil {
		return answer, err
	}
	execution := job.execution
	execution.Status = "completed"
	if err = s.store.SaveAnswer(job.ctx, s.cfg.RepositoryID, answer, execution); err != nil {
		return domain.Answer{}, err
	}
	return answer, nil
}

func (s *IntelligenceService) generateAnswer(ctx context.Context, snapshotID string, prepared preparedAnswer) (domain.Answer, error) {
	answer := domain.Answer{ID: newExecutionID(), SnapshotID: snapshotID, Model: s.model.Model(), CreatedAt: time.Now().UTC(), ContextKey: prepared.key}
	evidence, input, facts := prepared.evidence, prepared.input, prepared.facts
	answer.ContextTruncated = evidence.pack.Truncated || facts.Truncated || len(facts.Facts) < evidence.pack.Total
	if len(facts.Facts) == 0 {
		answer.Text, answer.InsufficientEvidence = insufficientAnswer, true
		answer.Limitations = modelLimitations(answer.Model, false)
		answer.Limitations = append(answer.Limitations, answerContextLimitations(evidence, answer.ContextTruncated)...)
		return answer, nil
	}
	response, err := s.answerData(ctx, input, evidence.flow != nil)
	if err != nil {
		return answer, err
	}
	answer.Model, answer.Cached = response.identity, response.cached
	answer.Limitations = answerModelLimitations(answer.Model, answer.Cached)
	answer, err = validateAnswer(response.data, answer, facts.Facts)
	if err != nil {
		return answer, err
	}
	answer.Limitations = append(answer.Limitations, answerContextLimitations(evidence, answer.ContextTruncated)...)
	return answer, response.save(ctx, s.cfg.RepositoryID)
}

func (s *IntelligenceService) answerData(ctx context.Context, input json.RawMessage, flow bool) (modelResponse, error) {
	purpose, task := "answers-v1", answerTask
	if flow {
		purpose, task = "answers-flow-v1", flowAnswerTask
	}
	response, err := s.inferenceCache(ctx, purpose, task, answerSchema, input)
	if err != nil {
		return response, err
	}
	if err := response.lookup(ctx, s.cfg.RepositoryID); err != nil || response.cached {
		return response, err
	}
	err = response.generate(ctx, s.model, task, answerSchema, input)
	return response, err
}

func validateAnswer(data json.RawMessage, answer domain.Answer, facts []domain.SymbolDetail) (domain.Answer, error) {
	var response struct {
		Text         *string   `json:"text"`
		SymbolIDs    *[]string `json:"symbol_ids"`
		Insufficient *bool     `json:"insufficient_evidence"`
	}
	if err := decodeModel(data, &response); err != nil {
		return answer, err
	}
	if response.Insufficient == nil || response.Text == nil || response.SymbolIDs == nil {
		return answer, domain.ErrInvalidModelOutput
	}
	if len(*response.Text) > 16000 {
		return answer, domain.ErrInvalidModelOutput
	}
	evidence, err := canonicalEvidence(*response.SymbolIDs, facts, !*response.Insufficient)
	if err != nil {
		return answer, err
	}
	answer.InsufficientEvidence = *response.Insufficient
	answer.Evidence = evidence
	if answer.InsufficientEvidence {
		answer.Text = insufficientAnswer
		return answer, nil
	}
	if !boundedText(*response.Text, 16000) {
		return answer, domain.ErrInvalidModelOutput
	}
	answer.Text = *response.Text
	return answer, nil
}
