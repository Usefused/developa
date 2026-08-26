package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"time"

	"developa/internal/application"
	source "developa/internal/source/git"
)

type scanOptions struct {
	repository    string
	watch         bool
	interval      time.Duration
	maxFileBytes  int64
	maxTotalBytes int64
}

func parseScanOptions(args []string, diagnostics io.Writer) (scanOptions, error) {
	var options scanOptions
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&options.repository, "repo", ".", "local Git repository to inspect")
	flags.BoolVar(&options.watch, "watch", false, "poll and emit one JSON report per source update")
	flags.DurationVar(&options.interval, "interval", 2*time.Second, "watch reconciliation interval")
	flags.Int64Var(&options.maxFileBytes, "max-file-bytes", 2<<20, "maximum eligible file size")
	flags.Int64Var(&options.maxTotalBytes, "max-total-bytes", 64<<20, "maximum captured source bytes")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	return validateOptions(options, flags.NArg())
}

func validateOptions(options scanOptions, remaining int) (scanOptions, error) {
	if remaining != 0 {
		return options, errors.New("unexpected positional arguments")
	}
	if options.interval <= 0 {
		return options, errors.New("interval must be positive")
	}
	if options.maxFileBytes <= 0 || options.maxTotalBytes < options.maxFileBytes {
		return options, errors.New("byte limits must be positive and total must cover one file")
	}
	return options, nil
}

func executeScan(ctx context.Context, options scanOptions, output io.Writer) error {
	scanner, err := application.Open(ctx, options.repository, source.Options{
		MaxFileBytes: options.maxFileBytes, MaxTotalBytes: options.maxTotalBytes,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	if options.watch {
		return watchScan(ctx, scanner, options.interval, encoder)
	}
	report, err := scanner.Scan(ctx)
	if err != nil {
		return err
	}
	return encoder.Encode(report)
}

func watchScan(ctx context.Context, scanner *application.Scanner, interval time.Duration, encoder *json.Encoder) error {
	err := scanner.Watch(ctx, interval, func(report application.Report) error { return encoder.Encode(report) })
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
