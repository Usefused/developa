package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"

	"developa/internal/domain"
)

const cloudOrigin = "https://ollama.com"
const cloudOutputInstruction = "\nReturn only valid JSON matching the following schema. Do not include markdown fences or prose outside JSON. Schema:\n"

var bearerPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+={0,2}$`)

func validateMode(cfg *Config) error {
	if cfg.Cloud {
		if err := validateCloudEndpoint(cfg.BaseURL); err != nil {
			return err
		}
		if len(cfg.APIKey) > 4096 || !bearerPattern.MatchString(cfg.APIKey) {
			return errors.New("Ollama Cloud requires a valid bearer API key")
		}
		cfg.BaseURL = cloudOrigin
		return nil
	}
	if cfg.APIKey != "" {
		return errors.New("local Ollama mode must not receive an API key")
	}
	return validateEndpoint(cfg.BaseURL)
}

func validateCloudEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("invalid Ollama Cloud endpoint")
	}
	if u.Scheme != "https" || !cloudHost(u.Host) {
		return errors.New("Ollama Cloud requires https://ollama.com")
	}
	return validateCloudOriginData(u)
}

func validateCloudOriginData(u *url.URL) error {
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("Ollama Cloud endpoint must not contain credentials or query data")
	}
	if u.Path != "" && u.Path != "/" {
		return errors.New("Ollama Cloud endpoint must be an origin URL")
	}
	if u.RawPath != "" {
		return errors.New("invalid Ollama Cloud endpoint path")
	}
	return nil
}

func cloudHost(host string) bool {
	return strings.EqualFold(host, "ollama.com") || strings.EqualFold(host, "ollama.com:443")
}

func cloudDial(ctx context.Context, network, address string) (net.Conn, error) {
	// This transport is never a general public-network client and ignores proxy environment variables.
	if !strings.EqualFold(address, "ollama.com:443") {
		return nil, domain.ErrModelUnavailable
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, "ollama.com:443")
}

func (c *Client) outputInstructions(system string, schema json.RawMessage) (string, json.RawMessage) {
	if !c.cfg.Cloud {
		return system, schema
	}
	// Ollama Cloud does not support structured-output format. Validation still runs after generation.
	return system + cloudOutputInstruction + string(schema), nil
}
