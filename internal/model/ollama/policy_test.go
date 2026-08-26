package ollama

import (
	"context"
	"developa/internal/domain"
	"errors"
	"strings"
	"testing"
)

func TestRejectUnsafeEndpointsAndModels(t *testing.T) {
	for _, endpoint := range []string{"https://ollama.com", "http://8.8.8.8:11434", "http://user:secret@localhost", "http://localhost/api", "http://localhost?secret=value", "file:///tmp/model"} {
		if _, err := New(Config{BaseURL: endpoint, Model: "fixture"}); err == nil {
			t.Fatalf("unsafe endpoint accepted: %q", endpoint)
		}
	}
	for _, model := range []string{"qwen:cloud", "local-cloud-alias", "https://remote/model", "", "model\nsecret"} {
		if _, err := New(Config{BaseURL: "http://localhost:11434", Model: model}); err == nil {
			t.Fatalf("unsafe model accepted: %q", model)
		}
	}
}

func TestDigestIsRequiredAndPinned(t *testing.T) {
	client, err := New(Config{BaseURL: "http://localhost:11434", Model: strings.Repeat("m", 128)})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.pinDigest(""); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal("missing digest accepted")
	}
	if err := client.pinDigest(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if len(client.Model()) != 200 {
		t.Fatal("model provenance exceeds storage contract")
	}
	if err := client.pinDigest(strings.Repeat("b", 64)); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal("mutable tag silently changed model")
	}
}

func TestPrivateHostConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:11434", "http://ollama:11434", "http://host.docker.internal:11434", "http://192.168.1.2:11434", "http://[::1]:11434"} {
		if _, err := New(Config{BaseURL: endpoint, Model: "fixture"}); err != nil {
			t.Fatalf("private endpoint rejected: %v", err)
		}
	}
	if _, err := privateDial(context.Background(), "tcp", "8.8.8.8:11434"); err == nil {
		t.Fatal("public connection permitted")
	}
}

func TestOutputRejectsTruncationAndToolCalls(t *testing.T) {
	for _, data := range []string{`{"done":false}`, `{"done":true,"done_reason":"length"}`, `{"done":true,"message":{"role":"assistant","content":"{}","tool_calls":[{}]}}`} {
		if _, err := chatContent([]byte(data)); err == nil {
			t.Fatal("unsupported model output accepted")
		}
	}
}
