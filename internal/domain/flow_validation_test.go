package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeFlowOptions(t *testing.T) {
	options, err := NormalizeFlowOptions(FlowOptions{})
	if err != nil || options.Depth != 6 || options.Limit != 80 {
		t.Fatal("flow defaults were not applied")
	}
	for _, selection := range []FlowOptions{{SymbolID: strings.Repeat("a", 64), Depth: 12, Limit: 100}, {FeatureID: strings.Repeat("b", 64), Depth: 1, Limit: 1}} {
		normalized, err := NormalizeFlowOptions(selection)
		if err != nil || normalized != selection {
			t.Fatal("valid explicit flow selection was changed")
		}
	}
}

func TestNormalizeFlowOptionsRejectsAmbiguousOrUnboundedSelection(t *testing.T) {
	cases := []FlowOptions{{Depth: -1}, {Depth: 13}, {Limit: -1}, {Limit: 101}, {SymbolID: "bad"},
		{FeatureID: strings.Repeat("A", 64)}, {SymbolID: strings.Repeat("a", 64), FeatureID: strings.Repeat("b", 64)}}
	for _, options := range cases {
		if _, err := NormalizeFlowOptions(options); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid flow selection was accepted: %+v", options)
		}
	}
}
