package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestResolveModelUsesMetadataOnlyForLocalAndCloud(t *testing.T) {
	for _, cloud := range []bool{false, true} {
		t.Run(fmt.Sprint(cloud), func(t *testing.T) { assertMetadataOnlyResolution(t, cloud) })
	}
}

func assertMetadataOnlyResolution(t *testing.T, cloud bool) {
	t.Helper()
	fixture := fixtureClient
	expected := "fixture@sha256:" + strings.Repeat("a", 64)
	if cloud {
		fixture, expected = cloudFixture, "fixture@cloud:aabbccddeeff"
	}
	var calls atomic.Int32
	client := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/api/tags" || r.Method != http.MethodGet || len(body) != 0 {
			t.Error("model resolution invoked inference or transferred request content")
		}
		if cloud {
			writeCloudTags(w, "aabbccddeeff")
			return
		}
		writeModelTags(w)
	}, nil)
	identity, err := client.ResolveModel(context.Background())
	if err != nil || identity != expected || calls.Load() != 1 {
		t.Fatalf("metadata resolution failed: %s %v", identity, err)
	}
}

func TestResolveModelRetainsCancellationAndDeadline(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, func(cfg *Config) {
		cfg.Timeout = 20 * time.Millisecond
	})
	if _, err := client.ResolveModel(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("metadata deadline was masked: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ResolveModel(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("metadata cancellation was masked: %v", err)
	}
}

func TestResolvedRevisionCannotChangeBeforeSourceTransfer(t *testing.T) {
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
	}, nil)
	if _, err := client.ResolveModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := client.Generate(context.Background(), "rules", "source", testSchema)
	if !errors.Is(err, domain.ErrModelUnavailable) || chats.Load() != 0 {
		t.Fatal("cache-preflight revision changed before source transfer")
	}
}

func TestGPTOSSUsesLowThinkingWithoutReturningProviderReasoning(t *testing.T) {
	client := cloudFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","digest":"aabbccddeeff"}]}`))
			return
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Think != "low" || request.Options["num_predict"] != float64(2048) {
			t.Error("GPT-OSS did not use its lowest supported thinking level and token bound")
		}
		_, _ = w.Write([]byte(`{"done":true,"message":{"role":"assistant","thinking":"PRIVATE-REASONING","content":"{\"ok\":true}"}}`))
	}, func(cfg *Config) { cfg.Model = "gpt-oss:20b" })
	data, err := client.Generate(context.Background(), "rules", "source", testSchema)
	if err != nil || string(data) != `{"ok":true}` {
		t.Fatal("GPT-OSS content contract failed or reasoning escaped")
	}
}

func TestThinkingPolicyUsesLevelsOnlyForGPTOSSFamily(t *testing.T) {
	for _, name := range []string{"gpt-oss", "gpt-oss:120b", "library/gpt-oss:20b", "gpt-oss-safeguard:20b"} {
		client := &Client{cfg: Config{Model: name}}
		if client.thinkingPolicy() != "low" {
			t.Fatal("GPT-OSS family did not select bounded thinking")
		}
	}
	for _, name := range []string{"qwen3.5:397b", "deepseek-v4-flash:0731", "other-gpt-oss:20b"} {
		client := &Client{cfg: Config{Model: name}}
		if client.thinkingPolicy() != false {
			t.Fatal("unrelated model enabled thinking")
		}
	}
}
