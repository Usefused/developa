package config

import (
	"testing"
	"time"
)

func TestIntelligenceOperatorSettings(t *testing.T) {
	cfg, err := load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_MODEL": "local-model", "OLLAMA_URL": "http://localhost:11434", "OLLAMA_TIMEOUT": "45s", "AI_TIMEOUT": "3m"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaModel != "local-model" || cfg.OllamaURL != "http://localhost:11434" || cfg.OllamaTimeout != 45*time.Second || cfg.AITimeout != 3*time.Minute {
		t.Fatal("intelligence settings not applied")
	}
}

func TestIntelligenceDefaultsDisableInference(t *testing.T) {
	cfg, err := load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaModel != "" || cfg.OllamaURL != "http://127.0.0.1:11434" || cfg.AITimeout != 2*time.Minute {
		t.Fatal("AI must default to local, explicitly configured models")
	}
	if cfg.OllamaCloud || cfg.OllamaAPIKey != "" {
		t.Fatal("cloud inference must require operator opt-in")
	}
}

func TestCloudSettingsUseExplicitOriginAndServerKey(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_CLOUD": "true", "OLLAMA_MODEL": "cloud-model", "OLLAMA_API_KEY": "test-secret"}
	cfg, err := load(environment(settings))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OllamaCloud || cfg.OllamaURL != "https://ollama.com" || cfg.OllamaAPIKey != "test-secret" {
		t.Fatal("cloud configuration was not applied")
	}
}

func TestAPIKeyAloneCannotEnableCloud(t *testing.T) {
	cfg, err := load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_API_KEY": "test-secret"}))
	if err != nil || cfg.OllamaCloud || cfg.OllamaURL != "http://127.0.0.1:11434" {
		t.Fatal("key presence must not change the selected inference endpoint")
	}
	_, err = load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_CLOUD": "test-secret"}))
	if err == nil || err.Error() != "OLLAMA_CLOUD must be a boolean" {
		t.Fatal("invalid opt-in must produce a sanitized configuration error")
	}
}

func TestOllamaCompatibilityBaseURLMapsToNativeOrigin(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_CLOUD": "true", "OLLAMA_BASE_URL": "https://ollama.com/v1/", "OLLAMA_MODEL": "deepseek-v4-flash"}
	cfg, err := load(environment(settings))
	if err != nil || cfg.OllamaURL != "https://ollama.com" || cfg.OllamaModel != "deepseek-v4-flash" {
		t.Fatal("compatibility endpoint was not normalized to native API origin")
	}
	settings["OLLAMA_URL"] = "https://ollama.com/api/"
	if _, err := load(environment(settings)); err != nil {
		t.Fatal("equivalent aliases must be accepted")
	}
	settings["OLLAMA_URL"] = "http://localhost:11434"
	if _, err := load(environment(settings)); err == nil {
		t.Fatal("conflicting endpoint variables must not silently override each other")
	}
}

func TestEndpointNormalizationPreservesUnsafePartsForPolicyValidation(t *testing.T) {
	for _, endpoint := range []string{"https://ollama.com/v1/?secret=value", "https://user:secret@ollama.com/v1/", "https://ollama.com/v1/#secret", "https://ollama.com/%76%31/"} {
		normalized, err := nativeOllamaEndpoint(endpoint)
		if err != nil || normalized == "https://ollama.com" {
			t.Fatal("normalization must not launder disallowed URL components")
		}
	}
}

func TestIntelligenceRejectsUnboundedTimeouts(t *testing.T) {
	for _, key := range []string{"OLLAMA_TIMEOUT", "AI_TIMEOUT"} {
		if _, err := load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db", key: "6m"})); err == nil {
			t.Fatalf("accepted unbounded %s", key)
		}
	}
}

func TestBackgroundIndexConfiguration(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db"}
	cfg, err := load(environment(settings))
	if err != nil || !cfg.AIIndexEnabled || cfg.AIPollInterval != 2*time.Second {
		t.Fatal("background indexing defaults were not applied")
	}
	settings["AI_INDEX_ENABLED"], settings["AI_POLL_INTERVAL"] = "false", "5s"
	cfg, err = load(environment(settings))
	if err != nil || cfg.AIIndexEnabled || cfg.AIPollInterval != 5*time.Second {
		t.Fatal("background indexing must be operator controllable")
	}
	settings["AI_INDEX_ENABLED"] = "invalid"
	if _, err := load(environment(settings)); err == nil {
		t.Fatal("invalid background switch accepted")
	}
}

func TestPurposeSpecificModelsOverrideSharedFallback(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_MODEL": "shared", "OLLAMA_ANALYSIS_MODEL": "small-analysis", "OLLAMA_FEATURE_MODEL": "feature-model", "OLLAMA_REVIEW_MODEL": "review-model", "OLLAMA_ANSWER_MODEL": "answer-model"}
	cfg, err := load(environment(settings))
	if err != nil || cfg.OllamaFeatureModel != "feature-model" || cfg.OllamaReviewModel != "review-model" || cfg.OllamaAnswerModel != "answer-model" {
		t.Fatal("task-specific models were not selected")
	}
}

func TestFeatureAndReviewModelsFallBackThroughLegacyAnalysisRole(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_MODEL": "shared", "OLLAMA_ANALYSIS_MODEL": "small-analysis", "OLLAMA_ANSWER_MODEL": "answer-model"}
	cfg, err := load(environment(settings))
	if err != nil || cfg.OllamaFeatureModel != "small-analysis" || cfg.OllamaReviewModel != "small-analysis" {
		t.Fatal("legacy analysis model must remain the feature and review fallback")
	}
}

func TestPurposeModelsFallBackToSharedModel(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_MODEL": "shared", "OLLAMA_ANSWER_MODEL": "answer-model"}
	cfg, err := load(environment(settings))
	if err != nil || cfg.OllamaAnalysisModel != "shared" || cfg.OllamaFeatureModel != "shared" || cfg.OllamaReviewModel != "shared" || cfg.OllamaAnswerModel != "answer-model" {
		t.Fatal("legacy model must be a fallback, not override task routing")
	}
}

func TestAutomaticFeaturesRequireSeparateOptIn(t *testing.T) {
	settings := map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_MODEL": "configured"}
	cfg, err := load(environment(settings))
	if err != nil || cfg.AIAutoFeatures || !cfg.AIIndexEnabled {
		t.Fatal("a configured model must enable manual jobs without automatic discovery")
	}
	settings["AI_AUTO_FEATURES"] = "true"
	cfg, err = load(environment(settings))
	if err != nil || !cfg.AIAutoFeatures {
		t.Fatal("automatic discovery opt-in was not applied")
	}
	settings["AI_AUTO_FEATURES"] = "invalid"
	if _, err := load(environment(settings)); err == nil {
		t.Fatal("invalid automatic discovery switch accepted")
	}
}

func TestIndependentModelRoleCanBeConfiguredWithoutFallback(t *testing.T) {
	cfg, err := load(environment(map[string]string{"DATABASE_URL": "postgres://localhost/db", "OLLAMA_FEATURE_MODEL": "strong-feature"}))
	if err != nil || cfg.OllamaFeatureModel != "strong-feature" || cfg.OllamaReviewModel != "" || cfg.OllamaAnswerModel != "" {
		t.Fatal("one model role must not implicitly enable the other")
	}
}
