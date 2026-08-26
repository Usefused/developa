package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestParseOptions(t *testing.T) {
	options, err := parseOptions([]string{"scan", "--repo", "/tmp/example", "--watch", "--interval", "1s"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.repository != "/tmp/example" || !options.watch {
		t.Fatalf("unexpected options: %+v", options)
	}
}

func TestParseOptionsRejectsInvalidArguments(t *testing.T) {
	tests := [][]string{
		nil, {"serve"}, {"scan", "extra"}, {"scan", "--interval", "0s"},
		{"scan", "--max-file-bytes", "0"}, {"scan", "--max-total-bytes", "1"},
	}
	for _, args := range tests {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}

func TestHelpDoesNotInitializeServices(t *testing.T) {
	var diagnostics strings.Builder
	if _, err := parseOptions([]string{"scan", "--help"}, &diagnostics); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected help, got %v", err)
	}
	if err := run(context.Background(), []string{"scan", "--help"}, io.Discard, &diagnostics); err != nil {
		t.Fatal(err)
	}
}
