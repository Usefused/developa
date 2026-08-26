package ollama

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"developa/internal/domain"
)

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func validateConfig(cfg Config) (Config, error) {
	if err := validateMode(&cfg); err != nil {
		return cfg, err
	}
	if !modelPattern.MatchString(cfg.Model) || strings.Contains(strings.ToLower(cfg.Model), "cloud") || strings.Contains(cfg.Model, "://") {
		return cfg, errors.New("an explicit Ollama model name without cloud alias suffix is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 256 << 10
	}
	if cfg.MaxPromptBytes == 0 {
		cfg.MaxPromptBytes = 24 << 10
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 1
	}
	if err := validateLimits(cfg); err != nil {
		return cfg, err
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return cfg, nil
}

func validateLimits(cfg Config) error {
	if cfg.Timeout <= 0 || cfg.Timeout > 5*time.Minute {
		return errors.New("Ollama timeout must be within five minutes")
	}
	if cfg.MaxResponseBytes < 1 || cfg.MaxResponseBytes > 1<<20 {
		return errors.New("invalid Ollama response limit")
	}
	if cfg.MaxPromptBytes < 1 || cfg.MaxPromptBytes > 24<<10 {
		return errors.New("invalid Ollama prompt limit")
	}
	if cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 4 {
		return errors.New("invalid Ollama concurrency limit")
	}
	return nil
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("invalid local Ollama endpoint")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("Ollama endpoint requires HTTP(S)")
	}
	if u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Ollama endpoint must not contain credentials or query data")
	}
	if u.Path != "" && u.Path != "/" {
		return errors.New("Ollama endpoint must be an origin URL")
	}
	return validateHost(u.Hostname())
}

func validateHost(host string) error {
	if strings.Contains(strings.ToLower(host), "ollama.com") {
		return errors.New("cloud Ollama endpoints are disabled")
	}
	if ip := net.ParseIP(host); ip != nil && !privateIP(ip) {
		return errors.New("Ollama endpoint must use a local or private address")
	}
	return nil
}

func privateIP(ip net.IP) bool { return ip.IsLoopback() || ip.IsPrivate() }

func privateDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, domain.ErrModelUnavailable
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, domain.ErrModelUnavailable
	}
	// Validate the actual resolved address on every new connection; no proxy or public fallback is used.
	for _, ip := range ips {
		if !privateIP(ip.IP) {
			return nil, domain.ErrModelUnavailable
		}
	}
	var dialer net.Dialer
	for _, ip := range ips {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, domain.ErrModelUnavailable
}

type installedModel struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	RemoteHost  string `json:"remote_host"`
	RemoteModel string `json:"remote_model"`
	Digest      string `json:"digest"`
}

func (c *Client) requireModel(ctx context.Context) error {
	// Preflight every source transfer, retaining provider revision pins in either explicit mode.
	data, err := c.request(ctx, http.MethodGet, "/api/tags", nil, 1<<20)
	if err != nil {
		return err
	}
	var response struct {
		Models []installedModel `json:"models"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return domain.ErrModelUnavailable
	}
	for _, model := range response.Models {
		if normalizedModel(model.Name) != normalizedModel(c.cfg.Model) {
			continue
		}
		if !c.cfg.Cloud && remoteModel(model) {
			return domain.ErrModelUnavailable
		}
		return c.pinDigest(model.Digest)
	}
	return domain.ErrModelUnavailable
}

func (c *Client) pinDigest(digest string) error {
	digest = strings.TrimPrefix(digest, "sha256:")
	decoded, err := hex.DecodeString(digest)
	if err != nil || !validDigestBytes(len(decoded), c.cfg.Cloud) {
		return domain.ErrModelUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A mutable tag cannot silently switch model versions within a discovery run.
	if c.digest != "" && c.digest != digest {
		return domain.ErrModelUnavailable
	}
	c.digest = digest
	return nil
}

func remoteModel(model installedModel) bool {
	return model.RemoteHost != "" || model.RemoteModel != "" || strings.Contains(strings.ToLower(model.Model), "cloud")
}

func validDigestBytes(size int, cloud bool) bool {
	if cloud {
		return size >= 6 && size <= 32
	}
	return size == 32
}

func normalizedModel(name string) string {
	last := name[strings.LastIndex(name, "/")+1:]
	if !strings.Contains(last, ":") {
		return name + ":latest"
	}
	return name
}
