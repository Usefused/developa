package main

import (
	"io"
	"testing"
)

func TestParseScanOptions(t *testing.T) {
	options, err := parseScanOptions([]string{"--repo", "/tmp/example", "--watch", "--interval", "1s"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.repository != "/tmp/example" || !options.watch {
		t.Fatalf("unexpected options: %+v", options)
	}
}

func TestParseScanOptionsRejectsInvalidArguments(t *testing.T) {
	tests := [][]string{
		{"extra"}, {"--interval", "0s"}, {"--max-file-bytes", "0"}, {"--max-total-bytes", "1"},
	}
	for _, args := range tests {
		if _, err := parseScanOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}
