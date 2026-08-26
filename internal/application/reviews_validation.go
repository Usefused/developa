package application

import (
	"encoding/json"
	"time"

	"developa/internal/domain"
)

type generatedReview struct {
	SymbolID     string                      `json:"symbol_id"`
	Summary      *string                     `json:"summary"`
	Parameters   *[]generatedParameterReview `json:"parameters"`
	Insufficient *bool                       `json:"insufficient_evidence"`
}

type generatedParameterReview struct {
	Position    *int    `json:"position"`
	Description *string `json:"description"`
}

func validateReviewBatch(data json.RawMessage, inputs []reviewEvidence, model string) ([]domain.FunctionReview, error) {
	var response struct {
		Reviews *[]generatedReview `json:"reviews"`
	}
	if err := decodeModel(data, &response); err != nil {
		return nil, err
	}
	if response.Reviews == nil || len(*response.Reviews) != len(inputs) {
		return nil, domain.ErrInvalidModelOutput
	}
	byID := make(map[string]reviewEvidence, len(inputs))
	for _, input := range inputs {
		byID[input.fact.Symbol.ID] = input
	}
	reviews := make([]domain.FunctionReview, 0, len(inputs))
	for _, generated := range *response.Reviews {
		input, ok := byID[generated.SymbolID]
		if !ok {
			return nil, domain.ErrInvalidModelOutput
		}
		delete(byID, generated.SymbolID)
		review, err := validateReview(generated, input, model)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

func validateReview(generated generatedReview, input reviewEvidence, model string) (domain.FunctionReview, error) {
	if !completeReview(generated, input.fact.Symbol.ID) {
		return domain.FunctionReview{}, domain.ErrInvalidModelOutput
	}
	parameters, err := reviewParameters(*generated.Parameters, input.input.Parameters)
	if !boundedText(*generated.Summary, 1200) || err != nil {
		return domain.FunctionReview{}, domain.ErrInvalidModelOutput
	}
	if *generated.Insufficient && len(*generated.Parameters) > 0 {
		return domain.FunctionReview{}, domain.ErrInvalidModelOutput
	}
	citation, err := canonicalCitation(input.fact)
	if err != nil {
		return domain.FunctionReview{}, err
	}
	summary := *generated.Summary
	if *generated.Insufficient {
		summary = "The captured source is insufficient to describe this function reliably."
	}
	return domain.FunctionReview{SymbolID: generated.SymbolID, SourceID: input.fact.Symbol.SourceID, Summary: summary, Parameters: parameters,
		InsufficientEvidence: *generated.Insufficient, ContextTruncated: input.input.Truncated, Model: model, PromptVersion: reviewVersion, CreatedAt: time.Now().UTC(), Evidence: citation}, nil
}

func completeReview(review generatedReview, id string) bool {
	return review.SymbolID == id && review.Summary != nil && review.Parameters != nil && review.Insufficient != nil
}

func reviewParameters(notes []generatedParameterReview, parameters []reviewParameter) ([]domain.ParameterReview, error) {
	if len(notes) > 16 {
		return nil, domain.ErrInvalidModelOutput
	}
	allowed := make(map[int]bool, len(parameters))
	for _, p := range parameters {
		allowed[p.Position] = true
	}
	result := make([]domain.ParameterReview, 0, len(notes))
	for _, note := range notes {
		if !validParameterNote(note, allowed) {
			return nil, domain.ErrInvalidModelOutput
		}
		delete(allowed, *note.Position)
		result = append(result, domain.ParameterReview{Position: *note.Position, Description: *note.Description})
	}
	return result, nil
}

func validParameterNote(note generatedParameterReview, allowed map[int]bool) bool {
	return note.Position != nil && note.Description != nil && allowed[*note.Position] && boundedText(*note.Description, 400)
}

func reviewCachePayload(review domain.FunctionReview) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"symbol_id": review.SymbolID, "summary": review.Summary, "parameters": review.Parameters, "insufficient_evidence": review.InsufficientEvidence})
	return data
}
