package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const maxGitOutput = 16 << 20

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > maxGitOutput-b.Len() {
		return 0, ErrLimitExceeded
	}
	return b.Buffer.Write(data)
}

func gitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+6)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			env = append(env, value)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_ATTR_NOSYSTEM=1", "LC_ALL=C")
}

func gitArguments(root string, args []string) []string {
	// Repository configuration must never turn an inspection into helper execution.
	prefix := []string{"--no-optional-locks", "--no-replace-objects", "-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull, "-c", "core.untrackedCache=false", "-c", "core.pager=",
		"-c", "diff.external=", "-c", "credential.helper=", "-c", "submodule.recurse=false",
		"-c", "core.attributesFile=" + os.DevNull,
		// Read-only container mounts can have a different owner. Trust only the operator-selected root.
		"-c", "safe.directory=", "-c", "safe.directory=" + root}
	return append(prefix, args...)
}

func runGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	overrides, err := filterOverrides(ctx, root)
	if err != nil {
		return nil, err
	}
	return runGitCommand(ctx, root, append(overrides, args...))
}

func runGitCommand(ctx context.Context, root string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", gitArguments(root, args)...)
	cmd.Dir, cmd.Env = root, gitEnvironment()
	var output boundedBuffer
	cmd.Stdout = &output
	// Stderr may contain paths and credentials from repository configuration.
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func filterOverrides(ctx context.Context, root string) ([]string, error) {
	// Even name-only diffs can invoke clean/process filters while checking dirty files.
	// Reading configuration is safe; all discovered filter drivers are disabled below.
	data, err := runGitCommand(ctx, root, []string{"config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|process|required)$`})
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return disableFilters(data)
}

func disableFilters(data []byte) ([]string, error) {
	drivers := make(map[string]bool)
	for _, key := range strings.Split(string(data), "\x00") {
		if key == "" {
			continue
		}
		if strings.ContainsAny(key, "=\r\n") {
			return nil, errors.New("unsupported Git filter configuration")
		}
		separator := strings.LastIndexByte(key, '.')
		if separator <= 7 || !strings.HasPrefix(key, "filter.") {
			return nil, errors.New("invalid Git filter key")
		}
		drivers[key[:separator]] = true
	}
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	var overrides []string
	for _, name := range names {
		overrides = append(overrides, "-c", name+".clean=", "-c", name+".process=", "-c", name+".required=false")
	}
	return overrides, nil
}

func optionalGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	value, err := runGit(ctx, root, args...)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil
	}
	return value, err
}

func gitFailure(operation string, err error) error {
	return fmt.Errorf("git %s: %w", operation, err)
}
