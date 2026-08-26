package golang

import (
	"context"
	"testing"
)

func FuzzParseNeverPanics(f *testing.F) {
	for _, source := range []string{"", "package p", "package p; type T struct {", "package p; func F[", "package p; var x = func() {", "package p; type I interface { M("} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		result, err := Parse(context.Background(), []SourceFile{{Path: "fuzz.go", Content: []byte(source)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) > 0 && result.Completeness != Partial {
			t.Fatal("syntax errors cannot produce a complete result")
		}
	})
}

func FuzzCallAnalysisNeverPanics(f *testing.F) {
	for _, source := range []string{"", "package p", "package p; func F() { F() }", "package p; var x = func(){}()", "package p; func F[", "package p; type I interface { M("} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		files := []SourceFile{{Path: "fuzz.go", Content: []byte(source)}}
		result, err := Parse(context.Background(), files)
		if err != nil {
			t.Fatal(err)
		}
		if err := AnalyzeCalls(context.Background(), files, &result); err != nil {
			t.Fatal(err)
		}
		for _, call := range result.Calls {
			if call.CallerID == "" || (call.Resolution == "resolved" && call.TargetID == "") {
				t.Fatal("call lost its source or resolved target identity")
			}
		}
	})
}
