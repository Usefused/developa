// Command openapi regenerates or checks the implemented HTTP contract without a running engine.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	httptransport "developa/internal/transport/http"
)

func main() {
	check := flag.Bool("check", false, "fail if the checked-in specification is stale")
	output := flag.String("output", "api/openapi.json", "specification file")
	flag.Parse()
	if err := generate(*output, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(path string, check bool) error {
	document, err := httptransport.OpenAPIDocument()
	if err != nil {
		return err
	}
	if !check {
		return os.WriteFile(path, document, 0644)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(saved, document) {
		return errors.New("OpenAPI is stale: run make api-generate")
	}
	return nil
}
