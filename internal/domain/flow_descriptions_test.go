package domain

import (
	"strings"
	"testing"
	"unicode/utf8"

	goparser "developa/internal/indexer/golang"
)

func TestFlowDescriptionPrefersCompiledBodyCommentsOverAI(t *testing.T) {
	symbol := goparser.Symbol{Kind: goparser.Function, Documentation: &goparser.Documentation{Summary: "Validate the request.\n\nReject empty input."}}
	description, source := reviewedFlowDescription(SymbolDetail{Symbol: symbol, Review: &FunctionReview{Summary: "AI suggestion"}})
	if description != "Validate the request. Reject empty input." || source != "source_comments" {
		t.Fatal("compiled comments did not take priority over AI")
	}
}

func TestFlowDescriptionsUseFirstSourceParagraph(t *testing.T) {
	cases := []struct {
		name, doc, comment, want string
	}{
		{"wrapped doc", "\r\n\tLoad reads the\r\n supplied values.\r\n \t\r\nSecond paragraph.", "Ignored trailing comment.", "Load reads the supplied values."},
		{"unicode", "\n名前は値を\n\t読み込みます。\n\n別の段落。", "", "名前は値を 読み込みます。"},
		{"comment fallback", " \n\t", "Trailing comment\n continues here.\n\nAnother paragraph.", "Trailing comment continues here."},
		{"lone carriage return", "First\rparagraph.\r\rSecond.", "", "First paragraph."},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			symbol := goparser.Symbol{Kind: goparser.Function, Doc: test.doc, Comment: test.comment}
			description, source := flowDescription(symbol)
			if description != test.want || source != "source_comment" {
				t.Fatalf("description = %q (%s), want %q (source_comment)", description, source, test.want)
			}
		})
	}
}

func TestFlowDescriptionCallableFacts(t *testing.T) {
	cases := []struct {
		name   string
		symbol goparser.Symbol
		want   string
	}{
		{"no arguments", goparser.Symbol{Kind: goparser.Function, Name: "DeleteAllRecords"}, "No parameters or return values."},
		{"context and error", goparser.Symbol{Kind: goparser.Method, Parameters: []goparser.Parameter{{Name: "ctx", Type: "context.Context"}}, Results: []goparser.Parameter{{Type: "error"}}}, "Accepts ctx (context.Context). Returns error."},
		{"variadic closure", goparser.Symbol{Kind: goparser.Closure, Parameters: []goparser.Parameter{{Name: "args", Type: "string", Variadic: true}}}, "Accepts args (...string). No return values."},
		{"named results", goparser.Symbol{Kind: goparser.InterfaceMethod, Results: []goparser.Parameter{{Name: "value", Type: "[]byte"}, {Type: "error"}}}, "No parameters. Returns value ([]byte), error."},
		{"unnamed and multiline", goparser.Symbol{Kind: goparser.Function, Parameters: []goparser.Parameter{{Type: "map[string]\n\tbool"}, {Type: "...int", Variadic: true}}}, "Accepts map[string] bool, ...int. No return values."},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) { assertFlowSignatureDescription(t, test.symbol, test.want) })
	}
}

func TestFlowDescriptionStructuralFacts(t *testing.T) {
	cases := []struct {
		name   string
		symbol goparser.Symbol
		want   string
	}{
		{"empty struct", goparser.Symbol{Kind: goparser.Struct}, "Struct with 0 declared fields."},
		{"single field", goparser.Symbol{Kind: goparser.Struct, Fields: []goparser.FieldInfo{{Name: "Name", Type: "string"}}}, "Struct with 1 declared field."},
		{"interface embeddings are not methods", goparser.Symbol{Kind: goparser.Interface, Fields: []goparser.FieldInfo{{Type: "io.Reader", Embedded: true}}}, "Interface declaration; method signatures are separate member records."},
		{"interface without embeddings", goparser.Symbol{Kind: goparser.Interface}, "Interface declaration; method signatures are separate member records."},
		{"alias", goparser.Symbol{Kind: goparser.Alias, Type: "[]byte"}, "Type alias of type []byte."},
		{"named type", goparser.Symbol{Kind: goparser.NamedType, Type: "string"}, "Named type of type string."},
		{"field", goparser.Symbol{Kind: goparser.Field, Type: "int"}, "Field of type int."},
		{"variable", goparser.Symbol{Kind: goparser.Variable, Name: "Enabled", Values: []string{"true"}}, "Variable declaration; type not explicitly declared."},
		{"constant", goparser.Symbol{Kind: goparser.Constant, Type: "time.Duration"}, "Constant of type time.Duration."},
		{"empty declaration", goparser.Symbol{}, "Declaration details are unavailable."},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) { assertFlowSignatureDescription(t, test.symbol, test.want) })
	}
}

func assertFlowSignatureDescription(t *testing.T, symbol goparser.Symbol, want string) {
	t.Helper()
	description, source := flowDescription(symbol)
	if description != want || source != "signature" {
		t.Fatalf("description = %q (%s), want %q (signature)", description, source, want)
	}
}

func TestFlowDescriptionBoundsUTF8(t *testing.T) {
	cases := []string{strings.Repeat("a", 500), strings.Repeat("界", 200), strings.Repeat("😀", 100), strings.Repeat("é", 200), strings.Repeat("\xffa", 400)}
	for _, doc := range cases {
		description, source := flowDescription(goparser.Symbol{Doc: doc})
		if len(description) > flowDescriptionBytes || !utf8.ValidString(description) || !strings.HasSuffix(description, "…") {
			t.Fatalf("description must truncate on a rune boundary: %q (%d bytes)", description, len(description))
		}
		if source != "source_comment" {
			t.Fatal("truncation changed description provenance")
		}
	}
	exact := strings.Repeat("界", flowDescriptionBytes/3)
	if got := boundedFlowDescription(exact); got != exact {
		t.Fatal("description at byte limit was needlessly truncated")
	}
}

func TestFlowDescriptionAnnotationPreservesSource(t *testing.T) {
	symbol := goparser.Symbol{ID: "symbol", Kind: goparser.Function, Doc: "First paragraph.\n\nFull second paragraph.", Comment: "Original comment."}
	flow := CodeFlow{Nodes: []FlowNode{{SymbolDetail: SymbolDetail{Symbol: symbol}}}}
	AnnotateFlow(&flow)
	node := flow.Nodes[0]
	if node.Description != "First paragraph." || node.DescriptionSource != "source_comment" {
		t.Fatal("flow annotation did not include the source preview")
	}
	if node.Symbol.Doc != symbol.Doc || node.Symbol.Comment != symbol.Comment {
		t.Fatal("preview replaced the full source documentation")
	}
	AnnotateFlow(&flow)
	if flow.Nodes[0].Description != node.Description {
		t.Fatal("description changed without a source change")
	}
}

func TestFlowDescriptionSignatureIsAlsoBounded(t *testing.T) {
	symbol := goparser.Symbol{Kind: goparser.Function, Parameters: []goparser.Parameter{{Name: "名", Type: strings.Repeat("長い型", 100)}}}
	description, source := flowDescription(symbol)
	if len(description) > flowDescriptionBytes || !utf8.ValidString(description) || !strings.HasSuffix(description, "…") || source != "signature" {
		t.Fatalf("unbounded signature description: %q (%s)", description, source)
	}
}
