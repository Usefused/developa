package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"developa/internal/application"
	source "developa/internal/source/git"
	"developa/internal/telemetry"
)

type scanOptions struct {
	repository    string
	watch         bool
	interval      time.Duration
	maxFileBytes  int64
	maxTotalBytes int64
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "developa:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	options, err := parseOptions(args, diagnostics)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	shutdown, err := telemetry.Setup(ctx, telemetry.Config{ServiceName: "developa-cli", Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")})
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	defer stopTelemetry(shutdown, diagnostics)
	return execute(ctx, options, output)
}

func parseOptions(args []string, diagnostics io.Writer) (scanOptions, error) {
	var options scanOptions
	if len(args) == 0 || args[0] != "scan" {
		return options, errors.New("usage: developa scan --repo /path/to/repository [--watch]")
	}
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&options.repository, "repo", ".", "local Git repository to inspect")
	flags.BoolVar(&options.watch, "watch", false, "poll and emit one JSON report per source update")
	flags.DurationVar(&options.interval, "interval", 2*time.Second, "watch reconciliation interval")
	flags.Int64Var(&options.maxFileBytes, "max-file-bytes", 2<<20, "maximum eligible file size")
	flags.Int64Var(&options.maxTotalBytes, "max-total-bytes", 64<<20, "maximum captured source bytes")
	if err := flags.Parse(args[1:]); err != nil {
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

func execute(ctx context.Context, options scanOptions, output io.Writer) error {
	scanner, err := application.Open(ctx, options.repository, source.Options{
		MaxFileBytes: options.maxFileBytes, MaxTotalBytes: options.maxTotalBytes,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	if options.watch {
		return watch(ctx, scanner, options.interval, encoder)
	}
	report, err := scanner.Scan(ctx)
	if err != nil {
		return err
	}
	return encoder.Encode(report)
}

func watch(ctx context.Context, scanner *application.Scanner, interval time.Duration, encoder *json.Encoder) error {
	err := scanner.Watch(ctx, interval, func(report application.Report) error { return encoder.Encode(report) })
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func stopTelemetry(shutdown func(context.Context) error, diagnostics io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		fmt.Fprintln(diagnostics, "developa: telemetry flush failed")
	}
}
