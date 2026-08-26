package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"developa/internal/domain"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var testSchema = json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`)

func writeModelTags(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"models":[{"name":"fixture:latest","model":"fixture:latest","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`))
}

func writeChat(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture:latest", "done": true, "done_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}})
}

func fixtureClient(t *testing.T, handler http.HandlerFunc, options func(*Config)) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := Config{BaseURL: server.URL, Model: "fixture"}
	if options != nil {
		options(&cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.http.CloseIdleConnections)
	return client
}

func TestStructuredChatProtocolAndLocalPreflight(t *testing.T) {
	var calls atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			calls.Add(1)
			writeModelTags(w)
			return
		}
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Error("unexpected endpoint")
		}
		if calls.Load() != 1 {
			t.Error("source transfer preceded model preflight")
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		assertChatRequest(t, request)
		writeChat(w, `{"ok":true}`)
	}, nil)
	data, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema)
	if err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("response: %s %v", data, err)
	}
}

func assertChatRequest(t *testing.T, request chatRequest) {
	t.Helper()
	if request.Stream || request.Think != false || !json.Valid(request.Format) {
		t.Error("structured non-streaming format missing")
	}
	if request.Options["temperature"] != float64(0) || request.Options["num_predict"] != float64(2048) {
		t.Error("generation limits missing")
	}
	if len(request.Messages) != 2 || request.Messages[1].Content != "SOURCE" {
		t.Error("prompt delivery mismatch")
	}
}

func TestCloudAliasCannotReceiveSource(t *testing.T) {
	for _, metadata := range []string{`"remote_host":"ollama.com"`, `"remote_model":"remote"`} {
		t.Run(metadata, func(t *testing.T) {
			var chats atomic.Int32
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					_, _ = fmt.Fprintf(w, `{"models":[{"name":"fixture:latest",%s}]}`, metadata)
					return
				}
				chats.Add(1)
			}, nil)
			_, err := client.Generate(context.Background(), "rules", "SECRET-SOURCE", testSchema)
			if !errors.Is(err, domain.ErrModelUnavailable) || chats.Load() != 0 {
				t.Fatal("cloud alias received source")
			}
		})
	}
}

func TestMissingModelAndProviderErrorsAreSafe(t *testing.T) {
	cases := []struct {
		name   string
		tags   string
		status int
	}{{"missing", `{"models":[]}`, 200}, {"failure", `PRIVATE-PROVIDER-BODY`, 500}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.tags))
			}, nil)
			_, err := client.Generate(context.Background(), "rules", "SECRET", testSchema)
			if !errors.Is(err, domain.ErrModelUnavailable) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

func TestMissingDigestPreventsSourceTransfer(t *testing.T) {
	var chats atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[{"name":"fixture:latest"}]}`))
			return
		}
		chats.Add(1)
	}, nil)
	if _, err := client.Generate(context.Background(), "rules", "SOURCE", testSchema); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
	if chats.Load() != 0 {
		t.Fatal("unidentified model received source")
	}
}

func TestDigestChangePreventsFurtherSourceTransfer(t *testing.T) {
	var tags, chats atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			digit := "a"
			if tags.Add(1) > 1 {
				digit = "b"
			}
			_, _ = fmt.Fprintf(w, `{"models":[{"name":"fixture:latest","digest":"%s"}]}`, strings.Repeat(digit, 64))
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
		t.Fatal("changed model received second source batch")
	}
}

func TestModelOutputLimitsAndInvalidPayloads(t *testing.T) {
	for _, content := range []string{`not json`, strings.Repeat("x", 2048)} {
		t.Run(fmt.Sprint(len(content)), func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					writeModelTags(w)
					return
				}
				writeChat(w, content)
			}, func(cfg *Config) { cfg.MaxResponseBytes = 512 })
			if _, err := client.Generate(context.Background(), "rules", "source", testSchema); !errors.Is(err, domain.ErrInvalidModelOutput) {
				t.Fatalf("invalid output accepted: %v", err)
			}
		})
	}
}

func TestCancellationAndDeadline(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, func(cfg *Config) { cfg.Timeout = 20 * time.Millisecond })
	if _, err := client.Generate(context.Background(), "rules", "source", testSchema); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout not preserved: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Generate(ctx, "rules", "source", testSchema); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
}

func TestDeadlineDuringResponseBodyIsPreserved(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			writeModelTags(w)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, func(cfg *Config) { cfg.Timeout = 30 * time.Millisecond })
	if _, err := client.Generate(context.Background(), "rules", "source", testSchema); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body timeout became invalid output: %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}, nil)
	if _, err := client.Generate(context.Background(), "rules", "source", testSchema); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("redirect was followed")
	}
}

func TestTelemetryDoesNotExportSourceOrProviderBody(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") == "" || r.Header.Get("baggage") != "" {
			t.Error("model request lost safe trace propagation")
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte("PRIVATE-BODY"))
	}, nil)
	_, _ = client.Generate(context.Background(), "PRIVATE-RULES", "PRIVATE-SOURCE", testSchema)
	spans := exporter.GetSpans()
	if len(spans) != 1 || strings.Contains(fmt.Sprint(spans), "PRIVATE") {
		t.Fatal("telemetry leaked sensitive content or missing span")
	}
}

type requestFixtureTransport func(*http.Request) (*http.Response, error)

func (f requestFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failedResponseBody struct {
	err    error
	closed bool
}

func (b *failedResponseBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failedResponseBody) Close() error             { b.closed = true; return nil }

func requestErrorFixture(t *testing.T, transport requestFixtureTransport) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: "http://127.0.0.1:11434", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = transport
	return client
}

func TestTransportDeadlineBeforeContextExpiryRetainsSafeCause(t *testing.T) {
	cases := []struct {
		name            string
		cause, expected error
	}{
		{"deadline", fmt.Errorf("PRIVATE deadline: %w", context.DeadlineExceeded), context.DeadlineExceeded},
		{"canceled", fmt.Errorf("PRIVATE cancellation: %w", context.Canceled), context.Canceled},
		{"header_timeout", &net.DNSError{Name: "PRIVATE", Err: "PRIVATE timeout", IsTimeout: true}, context.DeadlineExceeded},
		{"unavailable", errors.New("PRIVATE transport failure"), domain.ErrModelUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := requestErrorFixture(t, func(request *http.Request) (*http.Response, error) {
				if request.Context().Err() != nil {
					t.Fatal("fixture must exercise transport failure before outer context cancellation")
				}
				return nil, tc.cause
			})
			if _, err := client.ResolveModel(context.Background()); err != tc.expected {
				t.Fatalf("transport cause was masked or private details escaped: %v", err)
			}
		})
	}
}

func TestBodyDeadlineBeforeContextExpiryRetainsSafeCause(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		body := &failedResponseBody{err: fmt.Errorf("PRIVATE body: %w", cause)}
		client := requestErrorFixture(t, func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})
		_, err := client.ResolveModel(context.Background())
		if err != cause || !body.closed {
			t.Fatalf("body deadline was masked, leaked details, or lost cleanup: %v", err)
		}
	}
}

func TestCanceledRequestClosesResponseArrivingAtCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &failedResponseBody{err: errors.New("PRIVATE")}
	client := requestErrorFixture(t, func(request *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})
	if _, err := client.ResolveModel(ctx); err != context.Canceled || !body.closed {
		t.Fatal("cancellation lost its cause or left an arrived response body open")
	}
}
