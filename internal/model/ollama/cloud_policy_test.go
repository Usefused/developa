package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"developa/internal/domain"
)

func cloudConfig() Config {
	return Config{Cloud: true, BaseURL: cloudOrigin, Model: "fixture", APIKey: fakeCloudKey}
}

func TestCloudAcceptsOnlyExactHTTPSOrigin(t *testing.T) {
	for _, endpoint := range []string{"https://ollama.com", "https://ollama.com/", "https://OLLAMA.com:443/"} {
		cfg := cloudConfig()
		cfg.BaseURL = endpoint
		client, err := New(cfg)
		if err != nil {
			t.Fatalf("cloud origin rejected: %v", err)
		}
		if client.cfg.BaseURL != cloudOrigin {
			t.Fatal("cloud origin not canonicalized")
		}
		transport := client.http.Transport.(*http.Transport)
		if transport.Proxy != nil || transport.DialTLSContext != nil {
			t.Fatal("cloud transport can bypass origin restrictions")
		}
	}
}

func TestCloudRejectsUnsafeOrigins(t *testing.T) {
	endpoints := []string{
		"http://ollama.com", "https://ollama.com:8443", "https://ollama.com.evil.test", "https://api.ollama.com",
		"https://localhost", "https://user:secret@ollama.com", "https://ollama.com/api", "https://ollama.com/v1/",
		"https://ollama.com?secret=x", "https://ollama.com?", "https://ollama.com/#fragment", "https://ollama.com/%2f",
		"https://ollama.com./", "https://ollama.com:0443", "https://ollama.com\\@evil.test", "file:///ollama.com",
	}
	for _, endpoint := range endpoints {
		cfg := cloudConfig()
		cfg.BaseURL = endpoint
		if _, err := New(cfg); err == nil {
			t.Fatalf("unsafe cloud origin accepted: %q", endpoint)
		}
	}
}

func TestCloudBearerKeysAreSafeAndNeverSerialized(t *testing.T) {
	for _, key := range []string{"", " ", "key with spaces", "key\r\nAuthorization: stolen", "key\x00", strings.Repeat("a", 4097)} {
		cfg := cloudConfig()
		cfg.APIKey = key
		if _, err := New(cfg); err == nil {
			t.Fatal("unsafe bearer key accepted")
		}
	}
	cfg := cloudConfig()
	data, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(data), fakeCloudKey) || strings.Contains(string(data), "APIKey") {
		t.Fatal("API key appeared in serialized adapter configuration")
	}
}

func TestLocalModeRejectsCredentialsAndKeepsPrivateTransport(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://localhost:11434", Model: "fixture", APIKey: fakeCloudKey}); err == nil {
		t.Fatal("local mode accepted a credential it could leak")
	}
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("local request sent credentials")
		}
		if r.URL.Path == "/api/tags" {
			writeModelTags(w)
			return
		}
		writeChat(w, `{"ok":true}`)
	}, nil)
	if client.cfg.Cloud || client.http.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("default mode is not direct local inference")
	}
	if _, err := client.Generate(context.Background(), "rules", "source", testSchema); err != nil {
		t.Fatal(err)
	}
}

func TestCloudDialRejectsEveryOtherDestination(t *testing.T) {
	for _, address := range []string{"evil.test:443", "127.0.0.1:443", "ollama.com:80", "ollama.com.evil.test:443", "ollama.com:0443"} {
		if _, err := cloudDial(context.Background(), "tcp", address); !errors.Is(err, domain.ErrModelUnavailable) {
			t.Fatal("cloud transport accepted another destination")
		}
	}
}

func TestCloudProviderRevisionBoundsAndStorageContract(t *testing.T) {
	for _, digest := range []string{"abcdef123456", strings.Repeat("a", 64)} {
		cfg := cloudConfig()
		cfg.Model = strings.Repeat("m", 128)
		client, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.pinDigest(digest); err != nil {
			t.Fatal(err)
		}
		if client.Model() != cfg.Model+"@cloud:"+digest || len(client.Model()) > 200 {
			t.Fatal("cloud revision identity violates provenance or storage contract")
		}
	}
}

func TestCloudProviderRevisionRejectsMissingMalformedAndUnboundedValues(t *testing.T) {
	for _, digest := range []string{"", "aabbcc", "abcdef12345", "abcdefgh1234", strings.Repeat("a", 66)} {
		client, err := New(cloudConfig())
		if err != nil {
			t.Fatal(err)
		}
		if err := client.pinDigest(digest); !errors.Is(err, domain.ErrModelUnavailable) {
			t.Fatal("unusable cloud provider revision accepted")
		}
	}
	client, _ := New(Config{BaseURL: "http://localhost:11434", Model: "fixture"})
	if err := client.pinDigest("abcdef123456"); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal("cloud digest weakened local SHA256 requirement")
	}
}

func TestCloudUsesDirectModelNamesAndRejectsOffloadAliases(t *testing.T) {
	for _, model := range []string{"qwen3.5:397b", "kimi-k2.6", "gpt-oss:20b", "deepseek-v4-flash:0731"} {
		cfg := cloudConfig()
		cfg.Model = model
		if _, err := New(cfg); err != nil {
			t.Fatalf("direct cloud name rejected: %v", err)
		}
	}
	for _, model := range []string{"gpt-oss:120b-cloud", "fixture:cloud", strings.Repeat("m", 129)} {
		cfg := cloudConfig()
		cfg.Model = model
		if _, err := New(cfg); err == nil {
			t.Fatal("offload alias or unbounded model name accepted")
		}
	}
}
