package domain

import (
	"strings"
	"testing"
)

func TestReviewOptionsBoundPagesAndRejectAmbiguousSelectors(t *testing.T) {
	defaults, err := NormalizeReviewOptions(ReviewOptions{})
	if err != nil || defaults.Limit != 4 {
		t.Fatal("invalid review defaults")
	}
	for _, options := range []ReviewOptions{{Limit: 9}, {Limit: -1}, {Offset: -1}, {SymbolID: "bad"}, {SymbolID: strings.Repeat("a", 64), CalleeOf: strings.Repeat("b", 64)}} {
		if _, err := NormalizeReviewOptions(options); err == nil {
			t.Fatalf("accepted invalid options: %+v", options)
		}
	}
}

func TestReviewedFlowDescriptionKeepsCommentAndReviewSeparate(t *testing.T) {
	item := SymbolDetail{Review: &FunctionReview{Summary: "AI description."}}
	description, source := reviewedFlowDescription(item)
	if description != "AI description." || source != "llm_review" {
		t.Fatal("review not used when comment missing")
	}
	item.Symbol.Doc = "Source comment."
	description, source = reviewedFlowDescription(item)
	if description != "Source comment." || source != "source_comment" || item.Review.Summary != "AI description." {
		t.Fatal("review overwrote source documentation")
	}
	item.Symbol.Doc = ""
	item.Review.InsufficientEvidence = true
	_, source = reviewedFlowDescription(item)
	if source != "signature" {
		t.Fatal("abstention presented as a supported description")
	}
}
