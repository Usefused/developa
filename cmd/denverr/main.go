package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"developa/internal/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "denverr:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	if len(args) == 0 {
		writeUsage(diagnostics)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], diagnostics)
	case "scan":
		return runScan(ctx, args[1:], output, diagnostics)
	case "workspace":
		return runWorkspace(ctx, args[1:], output, diagnostics)
	case "version":
		return writeVersion(args[1:], output)
	case "help", "-h", "--help":
		writeUsage(output)
		return nil
	default:
		writeUsage(diagnostics)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runScan(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	options, err := parseScanOptions(args, diagnostics)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	return withTelemetry(ctx, diagnostics, func() error { return executeScan(ctx, options, output) })
}

func withTelemetry(ctx context.Context, diagnostics io.Writer, action func() error) error {
	shutdown, err := telemetry.Setup(ctx, telemetry.Config{ServiceName: "denverr-cli", Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")})
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	defer stopTelemetry(shutdown, diagnostics)
	return action()
}

func stopTelemetry(shutdown func(context.Context) error, diagnostics io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		fmt.Fprintln(diagnostics, "denverr: telemetry flush failed")
	}
}

func writeUsage(output io.Writer) {
	fmt.Fprintln(output, `Denverr indexes and explains Go repositories.

Usage:
  denverr serve [--database-url URL] [--workspace-root PATH ...]
  denverr scan [--repo PATH] [--watch]
  denverr workspace add [--database-url URL] [--name NAME] PATH
  denverr version`)
}
