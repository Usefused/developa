package main

import (
	"errors"
	"fmt"
	"io"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func writeVersion(args []string, output io.Writer) error {
	if len(args) != 0 {
		return errors.New("version does not accept arguments")
	}
	_, err := fmt.Fprintf(output, "denverr %s (commit %s, built %s)\n", version, commit, buildDate)
	return err
}
