package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"

	"developa/internal/config"
	"developa/internal/server"
)

type workspaceAddOptions struct {
	databaseURL string
	name        string
	path        string
}

func runWorkspace(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	if len(args) == 0 || args[0] != "add" {
		return errors.New("usage: denverr workspace add [--name NAME] PATH")
	}
	options, err := parseWorkspaceAddOptions(args[1:], diagnostics)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	return withTelemetry(ctx, diagnostics, func() error { return addWorkspace(ctx, options, output) })
}

func parseWorkspaceAddOptions(args []string, diagnostics io.Writer) (workspaceAddOptions, error) {
	var options workspaceAddOptions
	flags := flag.NewFlagSet("workspace add", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&options.databaseURL, "database-url", "", "PostgreSQL URL; defaults to DATABASE_URL")
	flags.StringVar(&options.name, "name", "", "workspace display name")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 1 {
		return options, errors.New("workspace add requires exactly one repository path")
	}
	path, err := filepath.Abs(flags.Arg(0))
	options.path = path
	return options, err
}

func addWorkspace(ctx context.Context, options workspaceAddOptions, output io.Writer) error {
	databaseURL := options.databaseURL
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		return errors.New("DATABASE_URL or --database-url is required")
	}
	cfg, err := config.LoadWithOverrides(workspaceCommandOverrides(databaseURL))
	if err != nil {
		return err
	}
	result, err := server.RegisterWorkspace(ctx, cfg, options.path, options.name)
	if err != nil {
		return err
	}
	response := struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		AlreadyAdded    bool   `json:"already_added"`
		RestartRequired bool   `json:"restart_required"`
	}{ID: result.ID, Name: result.Name, AlreadyAdded: result.AlreadyAdded, RestartRequired: true}
	return json.NewEncoder(output).Encode(response)
}

func workspaceCommandOverrides(databaseURL string) map[string]string {
	return map[string]string{
		"DATABASE_URL": databaseURL, "WORKSPACE_ROOTS": "", "REPOSITORIES": "", "REPOSITORY_PATH": "", "REPOSITORY_NAME": "",
		"DENVERR_API_TOKEN": "", "OLLAMA_CLOUD": "false", "OLLAMA_URL": "", "OLLAMA_BASE_URL": "", "OLLAMA_API_KEY": "",
		"OLLAMA_MODEL": "", "OLLAMA_ANALYSIS_MODEL": "", "OLLAMA_FEATURE_MODEL": "", "OLLAMA_REVIEW_MODEL": "", "OLLAMA_ANSWER_MODEL": "", "AI_INDEX_ENABLED": "false", "AI_AUTO_FEATURES": "false",
	}
}
