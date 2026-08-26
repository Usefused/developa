package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"developa/internal/domain"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const fakeCloudKey = "fake-cloud-test-key"

// The test transport maps the validated cloud origin to a local TLS fixture; production has no override.
type cloudFixtureTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (t cloudFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "ollama.com" {
		return nil, errors.New("unexpected test origin")
	}
	mapped := request.Clone(request.Context())
	u := *request.URL
	u.Scheme, u.Host = t.target.Scheme, t.target.Host
	mapped.URL, mapped.Host = &u, t.target.Host
	return t.next.RoundTrip(mapped)
}

func cloudFixture(t *testing.T, handler http.HandlerFunc, options func(*Config)) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	t.Cleanup(server.Client().CloseIdleConnections)
	cfg := Config{Cloud: true, BaseURL: cloudOrigin, APIKey: fakeCloudKey, Model: "fixture"}
	if options != nil {
		options(&cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = cloudFixtureTransport{target: target, next: server.Client().Transport}
	return client
}

func writeCloudTags(w http.ResponseWriter, digest string) {
	_, _ = fmt.Fprintf(w, `{"models":[{"name":"fixture","model":"fixture","digest":%q}]}`, digest)
}

func TestCloudChatUsesBearerAndPromptSchemaWithoutFormat(t *testing.T) {
	var tags, chats atomic.Int32
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeCloudKey {
			t.Error("missing explicit cloud authorization")
		}
		if r.URL.Path == "/api/tags" {
			tags.Add(1)
			writeCloudTags(w, "abcdef123456")
			return
		}
		chats.Add(1)
		assertCloudEndpoint(t, r, tags.Load())
		assertCloudRequest(t, r)
		writeChat(w, `{"ok":true}`)
	}, nil)
	if client.Model() != "fixture@cloud:unverified" {
		t.Fatal("unverified identity hides cloud backend")
	}
	data, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema)
	if err != nil || string(data) != `{"ok":true}` || chats.Load() != 1 {
		t.Fatalf("cloud response: %s %v", data, err)
	}
	if client.Model() != "fixture@cloud:abcdef123456" {
		t.Fatal("cloud provider revision was not recorded honestly")
	}
}

func assertCloudEndpoint(t *testing.T, r *http.Request, tags int32) {
	t.Helper()
	if tags != 1 || r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
		t.Error("cloud preflight/chat order or endpoint incorrect")
	}
}

func assertCloudRequest(t *testing.T, r *http.Request) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["format"]; exists {
		t.Error("cloud received unsupported structured-output format")
	}
	data, _ := json.Marshal(raw)
	var request chatRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	// Reuse the local request checks for the unchanged stream/token/source contract.
	request.Format = testSchema
	assertChatRequest(t, request)
	if len(request.Messages) != 2 {
		t.Fatal("system and user messages required")
	}
	if request.Messages[0].Content != "rules"+cloudOutputInstruction+string(testSchema) {
		t.Error("cloud schema and JSON-only instructions missing")
	}
}

func TestCloudRevisionChangeStopsFurtherSourceTransfer(t *testing.T) {
	var tags, chats atomic.Int32
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			digest := "abcdef123456"
			if tags.Add(1) > 1 {
				digest = "abcdef123457"
			}
			writeCloudTags(w, digest)
			return
		}
		chats.Add(1)
		writeChat(w, `{"ok":true}`)
	}, nil)
	if _, err := client.Generate(context.Background(), "rules", "FIRST", testSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), "rules", "SECOND", testSchema); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
	if chats.Load() != 1 {
		t.Fatal("changed cloud revision received more source")
	}
}

func TestCloudMissingRevisionPreventsSourceTransfer(t *testing.T) {
	var chats atomic.Int32
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			writeCloudTags(w, "")
			return
		}
		chats.Add(1)
	}, nil)
	_, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema)
	if !errors.Is(err, domain.ErrModelUnavailable) || chats.Load() != 0 {
		t.Fatal("cloud model without revision received source")
	}
}

func TestCloudRejectsInvalidOutputWithoutRepairOrRetry(t *testing.T) {
	for _, output := range []string{"```json\n{}\n```", "not JSON", strings.Repeat("x", 2048)} {
		t.Run(fmt.Sprint(len(output)), func(t *testing.T) {
			var chats atomic.Int32
			client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					writeCloudTags(w, "abcdef123456")
					return
				}
				chats.Add(1)
				writeChat(w, output)
			}, func(cfg *Config) { cfg.MaxResponseBytes = 512 })
			_, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema)
			if !errors.Is(err, domain.ErrInvalidModelOutput) || chats.Load() != 1 {
				t.Fatalf("invalid cloud output repaired or accepted: %v", err)
			}
		})
	}
}

func TestCloudSchemaInstructionCountsTowardPromptBudget(t *testing.T) {
	var calls atomic.Int32
	client := cloudFixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) }, func(cfg *Config) {
		cfg.MaxPromptBytes = len(testSchema) + len("rulesSOURCE")
	})
	_, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema)
	if !errors.Is(err, domain.ErrInvalidInput) || calls.Load() != 0 {
		t.Fatal("cloud schema instruction bypassed prompt budget")
	}
}

func TestCloudRedirectCannotForwardCredentialsOrSource(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			writeCloudTags(w, "abcdef123456")
			return
		}
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}, nil)
	_, err := client.Generate(context.Background(), "rules", "PRIVATE-SOURCE", testSchema)
	if !errors.Is(err, domain.ErrModelUnavailable) || destinationCalls.Load() != 0 {
		t.Fatal("cloud redirect permitted source or credential forwarding")
	}
}

func TestCloudCancellationAndDeadline(t *testing.T) {
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			writeCloudTags(w, "abcdef123456")
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, func(cfg *Config) { cfg.Timeout = 60 * time.Millisecond })
	if _, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cloud deadline not preserved: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Generate(ctx, "rules", "SOURCE", testSchema); !errors.Is(err, context.Canceled) {
		t.Fatalf("cloud cancellation not preserved: %v", err)
	}
}

func TestCloudFailureTelemetryHasModeButNoCredentialsOrSource(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") == "" || r.Header.Get("baggage") != "" {
			t.Error("cloud trace propagation unsafe or missing")
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("PRIVATE-PROVIDER-BODY " + fakeCloudKey))
	}, nil)
	_, err := client.Generate(context.Background(), "PRIVATE-RULES", "PRIVATE-SOURCE", testSchema)
	if !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal("provider auth error was not bounded")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatal("missing cloud execution trace")
	}
	serialized := fmt.Sprint(spans, err)
	if strings.Contains(serialized, "PRIVATE") || strings.Contains(serialized, fakeCloudKey) {
		t.Fatal("cloud telemetry or errors leaked sensitive data")
	}
	assertCloudSpan(t, spans[0])
}

func assertCloudSpan(t *testing.T, span tracetest.SpanStub) {
	t.Helper()
	for _, attr := range span.Attributes {
		if attr.Key == "model.cloud" && attr.Value.AsBool() {
			return
		}
	}
	t.Error("cloud source transfer was not explicit in telemetry")
}
