package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"developa/internal/config"
	"developa/internal/localconfig"
	"developa/internal/server"
)

type serveOptions struct {
	databaseURL string
	listen      string
	roots       stringList
}

type stringList []string

func (values *stringList) String() string { return fmt.Sprint([]string(*values)) }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runServe(ctx context.Context, args []string, diagnostics io.Writer) error {
	options, err := parseServeOptions(args, diagnostics)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	cfg, token, err := serveConfig(options)
	if err != nil {
		return err
	}
	if token.Created {
		fmt.Fprintf(diagnostics, "Denverr access token (shown on first creation): %s\nSaved to %s\n", token.Value, token.Path)
	}
	return server.Run(ctx, cfg)
}

func parseServeOptions(args []string, diagnostics io.Writer) (serveOptions, error) {
	var options serveOptions
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&options.databaseURL, "database-url", "", "PostgreSQL URL; DATABASE_URL is safer for URLs containing passwords")
	flags.StringVar(&options.listen, "listen", "", "HTTP address (default 127.0.0.1:8080)")
	flags.Var(&options.roots, "workspace-root", "folder users may browse for Git repositories; repeatable")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errors.New("serve does not accept positional arguments")
	}
	return options, nil
}

func serveConfig(options serveOptions) (config.Config, localconfig.Token, error) {
	databaseURL := options.databaseURL
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		return config.Config{}, localconfig.Token{}, errors.New("DATABASE_URL or --database-url is required")
	}
	token, err := localconfig.LoadOrCreateToken(os.Getenv("DENVERR_API_TOKEN"))
	if err != nil {
		return config.Config{}, token, err
	}
	overrides := map[string]string{"DATABASE_URL": databaseURL, "DENVERR_API_TOKEN": token.Value}
	if options.listen != "" {
		overrides["HTTP_ADDR"] = options.listen
	}
	if roots, ok, err := workspaceRootJSON(options.roots); err != nil {
		return config.Config{}, token, err
	} else if ok {
		overrides["WORKSPACE_ROOTS"] = roots
	}
	cfg, err := config.LoadWithOverrides(overrides)
	return cfg, token, err
}

func workspaceRootJSON(configured []string) (string, bool, error) {
	if len(configured) == 0 && hasEnvironmentWorkspace() {
		return "", false, nil
	}
	if len(configured) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false, errors.New("current workspace root is unavailable")
		}
		configured = []string{cwd}
	}
	roots, err := canonicalRoots(configured)
	if err != nil {
		return "", false, err
	}
	payload, err := json.Marshal(roots)
	return string(payload), true, err
}

func hasEnvironmentWorkspace() bool {
	return os.Getenv("WORKSPACE_ROOTS") != "" || os.Getenv("REPOSITORIES") != "" || os.Getenv("REPOSITORY_PATH") != ""
}

func canonicalRoots(paths []string) ([]string, error) {
	if len(paths) > 16 {
		return nil, errors.New("at most 16 workspace roots may be configured")
	}
	roots, seen := make([]string, 0, len(paths)), map[string]bool{}
	for _, path := range paths {
		root, err := canonicalRoot(path)
		if err != nil {
			return nil, err
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots, nil
}

func canonicalRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("workspace root is unavailable")
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", errors.New("workspace root is unavailable")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("workspace root must be an existing directory")
	}
	return root, nil
}
