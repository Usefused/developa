// Package ollama defaults to local inference, with explicit operator opt-in for Ollama Cloud.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"developa/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

type Config struct {
	BaseURL          string
	Model            string
	Cloud            bool
	APIKey           string `json:"-"`
	Timeout          time.Duration
	MaxResponseBytes int64
	MaxPromptBytes   int
	MaxConcurrent    int
}

type Client struct {
	cfg    Config
	http   *http.Client
	slots  chan struct{}
	mu     sync.Mutex
	digest string
}

func New(cfg Config) (*Client, error) {
	var err error
	cfg, err = validateConfig(cfg)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: privateDial, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: cfg.Timeout}
	if cfg.Cloud {
		transport.DialContext = cloudDial
	}
	client := &http.Client{Transport: transport, Timeout: cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Ollama redirects are disabled") }}
	return &Client{cfg: cfg, http: client, slots: make(chan struct{}, cfg.MaxConcurrent)}, nil
}

func (c *Client) Model() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.digest == "" {
		if c.cfg.Cloud {
			return c.cfg.Model + "@cloud:unverified"
		}
		return c.cfg.Model
	}
	if c.cfg.Cloud {
		return c.cfg.Model + "@cloud:" + c.digest
	}
	return c.cfg.Model + "@sha256:" + c.digest
}

func (c *Client) Generate(ctx context.Context, system, prompt string, schema json.RawMessage) (result json.RawMessage, err error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	ctx, span := otel.Tracer("denverr/model/ollama").Start(ctx, "ollama.generate")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, "model request failed")
			span.AddEvent("model.failed")
		} else {
			span.AddEvent("model.completed")
		}
		span.End()
	}()
	span.SetAttributes(attribute.String("model.provider", "ollama"), attribute.String("model.name", c.cfg.Model), attribute.Bool("model.cloud", c.cfg.Cloud))
	if !json.Valid(schema) {
		return nil, domain.ErrInvalidInput
	}
	system, schema = c.outputInstructions(system, schema)
	if len(system)+len(prompt)+len(schema) > c.cfg.MaxPromptBytes {
		return nil, domain.ErrInvalidInput
	}
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.slots }()
	if err := c.requireModel(ctx); err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.String("model.identity", c.Model()))
	return c.chat(ctx, system, prompt, schema)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model    string          `json:"model"`
	Messages []message       `json:"messages"`
	Format   json.RawMessage `json:"format,omitempty"`
	Stream   bool            `json:"stream"`
	Think    any             `json:"think"`
	Options  map[string]any  `json:"options"`
}

func (c *Client) chat(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: []message{{Role: "system", Content: system}, {Role: "user", Content: prompt}},
		Format: schema, Think: c.thinkingPolicy(), Options: map[string]any{"temperature": 0, "num_predict": 2048, "num_ctx": 32768}})
	if err != nil {
		return nil, domain.ErrInvalidInput
	}
	data, err := c.request(ctx, http.MethodPost, "/api/chat", body, c.cfg.MaxResponseBytes)
	if err != nil {
		return nil, err
	}
	return chatContent(data)
}

func chatContent(data []byte) (json.RawMessage, error) {
	var response struct {
		Model      string `json:"model"`
		Done       bool   `json:"done"`
		DoneReason string `json:"done_reason"`
		Error      string `json:"error"`
		Message    struct {
			Role      string            `json:"role"`
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, domain.ErrInvalidModelOutput
	}
	if !response.Done || response.Error != "" || response.DoneReason == "length" {
		return nil, domain.ErrInvalidModelOutput
	}
	if response.Message.Role != "assistant" || len(response.Message.ToolCalls) != 0 {
		return nil, domain.ErrInvalidModelOutput
	}
	if !json.Valid([]byte(response.Message.Content)) {
		return nil, domain.ErrInvalidModelOutput
	}
	return json.RawMessage(response.Message.Content), nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, domain.ErrModelUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Cloud {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	// Propagate trace identity only; arbitrary caller baggage may contain sensitive source data.
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	response, err := c.http.Do(req)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil || ctx.Err() != nil {
		return nil, requestFailure(ctx, err, domain.ErrModelUnavailable)
	}
	if response.StatusCode != http.StatusOK {
		return nil, domain.ErrModelUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || ctx.Err() != nil {
		return nil, requestFailure(ctx, err, domain.ErrInvalidModelOutput)
	}
	if int64(len(data)) > limit {
		return nil, domain.ErrInvalidModelOutput
	}
	return data, nil
}

func requestFailure(ctx context.Context, err, fallback error) error {
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	// HTTP client/header/body deadlines can win before the operation context's
	// timer runs. Preserve their cause without returning URLs or transport details.
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return context.DeadlineExceeded
	}
	return fallback
}
