package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"developa/internal/domain"
)

func answerTestModel(id string) *intelligenceTestModel {
	return &intelligenceTestModel{generate: func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"text": "The function returns without performing additional work.", "symbol_ids": []string{id}, "insufficient_evidence": false})
	}}
}

func TestAnswerUsesExactSymbolContextAndCanonicalCitation(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(3)}
	id := store.symbols[1].Symbol.ID
	service := fixtureIntelligence(t, store, answerTestModel(id), IntelligenceConfig{})
	answer, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "What does this function do?", SymbolID: id})
	if err != nil {
		t.Fatal(err)
	}
	if store.symbolReads != 1 || store.contextReads != 0 || answer.Evidence[0].Path != "file1.go" {
		t.Fatal("answer did not use exact canonical source context")
	}
	if answer.InsufficientEvidence || store.answer.ID != answer.ID {
		t.Fatal("supported answer was not persisted")
	}
	assertIntelligenceAudit(t, store, "completed")
}

func TestAnswerUsesBoundedSQLContext(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(5)}
	service := fixtureIntelligence(t, store, answerTestModel(store.symbols[0].Symbol.ID), IntelligenceConfig{BatchSize: 2})
	answer, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "What runs?"})
	if err != nil || !answer.ContextTruncated || store.contextReads != 1 || store.symbolReads != 0 {
		t.Fatalf("context: %+v %v", answer, err)
	}
}

func TestAnswerAbstainsWithoutContextWithoutCallingModel(t *testing.T) {
	store := &intelligenceTestStore{}
	model := answerTestModel("unused")
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	answer, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Unknown functionality?"})
	if err != nil || !answer.InsufficientEvidence || model.calls.Load() != 0 || answer.Text != insufficientAnswer {
		t.Fatalf("abstention: %+v %v", answer, err)
	}
	assertIntelligenceAudit(t, store, "completed")
}

func TestModelAbstentionCannotSmuggleUnsupportedAnswer(t *testing.T) {
	answer, err := validateAnswer(json.RawMessage(`{"text":"Unsupported confident claim","symbol_ids":[],"insufficient_evidence":true}`), domain.Answer{}, intelligenceFacts(1))
	if err != nil || answer.Text != insufficientAnswer {
		t.Fatal("abstention repeated unsupported model claims")
	}
}

func TestGroundingRejectsMissingInventedAndExtraFields(t *testing.T) {
	facts := intelligenceFacts(1)
	for _, data := range []string{`{"text":"claim","symbol_ids":[],"insufficient_evidence":false}`, `{"text":"claim","symbol_ids":["invented"],"insufficient_evidence":false}`, `{"text":"claim","symbol_ids":[],"insufficient_evidence":true,"path":"invented.go"}`, `{"text":"claim"}`} {
		if _, err := validateAnswer([]byte(data), domain.Answer{}, facts); !errors.Is(err, domain.ErrInvalidModelOutput) {
			t.Fatalf("ungrounded answer accepted: %v", err)
		}
	}
	for _, data := range []string{`{}`, `{"features":null}`, `{"features":[{"title":"x","summary":"x","symbol_ids":[]}]}`, `{"features":[],"instructions":"do something"}`} {
		if _, err := validateFeatures([]byte(data), facts, "run", 0); !errors.Is(err, domain.ErrInvalidModelOutput) {
			t.Fatalf("invalid feature output accepted: %v", err)
		}
	}
}

func TestAnswerStorageFailureDoesNotClaimCompletion(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1), saveError: errors.New("storage unavailable")}
	service := fixtureIntelligence(t, store, answerTestModel(store.symbols[0].Symbol.ID), IntelligenceConfig{})
	if _, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain"}); err == nil {
		t.Fatal("unsaved answer reported success")
	}
	assertIntelligenceAudit(t, store, "error")
}

func TestCloudAnswerPersistsHonestProviderAndTransferProvenance(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1)}
	model := answerTestModel(store.symbols[0].Symbol.ID)
	model.identity = "shared-model@cloud:unverified"
	generate := model.generate
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		model.identity = "shared-model@cloud:aabbccddeeff"
		return generate(ctx, system, prompt, schema)
	}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	answer, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain"})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Model != "shared-model@cloud:aabbccddeeff" {
		t.Fatalf("cloud revision was mislabeled: %s", answer.Model)
	}
	assertCloudProvenance(t, answer.Limitations)
	assertCloudProvenance(t, store.answer.Limitations)
}

func TestCloudAnswerWithoutEvidenceDoesNotClaimSourceTransfer(t *testing.T) {
	store := &intelligenceTestStore{}
	model := answerTestModel("unused")
	model.identity = "shared-model@cloud:unverified"
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	answer, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Unknown functionality?"})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls.Load() != 0 || !answer.InsufficientEvidence || !slices.Contains(answer.Limitations, cloudNoTransferLimitation) {
		t.Fatal("no-context cloud answer misrepresented inference")
	}
	if slices.Contains(answer.Limitations, cloudTransferLimitation) || slices.Contains(answer.Limitations, cloudRevisionLimitation) {
		t.Fatal("no model invocation must not claim source transfer or a verified provider revision")
	}
}

func TestLocalAnswerDoesNotAcquireCloudLimitations(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1)}
	service := fixtureIntelligence(t, store, answerTestModel(store.symbols[0].Symbol.ID), IntelligenceConfig{})
	answer, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain"})
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Limitations) != 0 {
		t.Fatalf("local inference acquired a cloud transfer claim: %+v", answer.Limitations)
	}
}
