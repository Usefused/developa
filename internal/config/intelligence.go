package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func loadIntelligence(lookup lookupEnv, cfg *Config) error {
	if err := loadOllamaMode(lookup, cfg); err != nil {
		return err
	}
	if err := loadAnalysisWorker(lookup, cfg); err != nil {
		return err
	}
	if err := loadOllamaModels(lookup, cfg); err != nil {
		return err
	}
	var modelErr, executionErr error
	cfg.OllamaTimeout, modelErr = duration(lookup, "OLLAMA_TIMEOUT", time.Minute)
	cfg.AITimeout, executionErr = duration(lookup, "AI_TIMEOUT", 2*time.Minute)
	return errors.Join(modelErr, executionErr)
}

func loadOllamaModels(lookup lookupEnv, cfg *Config) error {
	cfg.OllamaModel = value(lookup, "OLLAMA_MODEL", "")
	cfg.OllamaAnalysisModel = value(lookup, "OLLAMA_ANALYSIS_MODEL", cfg.OllamaModel)
	cfg.OllamaFeatureModel = value(lookup, "OLLAMA_FEATURE_MODEL", cfg.OllamaAnalysisModel)
	cfg.OllamaReviewModel = value(lookup, "OLLAMA_REVIEW_MODEL", cfg.OllamaAnalysisModel)
	cfg.OllamaAnswerModel = value(lookup, "OLLAMA_ANSWER_MODEL", cfg.OllamaModel)
	models := map[string]string{"OLLAMA_MODEL": cfg.OllamaModel, "OLLAMA_ANALYSIS_MODEL": cfg.OllamaAnalysisModel, "OLLAMA_FEATURE_MODEL": cfg.OllamaFeatureModel,
		"OLLAMA_REVIEW_MODEL": cfg.OllamaReviewModel, "OLLAMA_ANSWER_MODEL": cfg.OllamaAnswerModel}
	for name, model := range models {
		if strings.ContainsRune(model, 0) || len(model) > 128 {
			return fmt.Errorf("%s must be a valid model name of at most 128 bytes", name)
		}
	}
	return nil
}

func loadAnalysisWorker(lookup lookupEnv, cfg *Config) error {
	enabled, err := strconv.ParseBool(value(lookup, "AI_INDEX_ENABLED", "true"))
	if err != nil {
		return errors.New("AI_INDEX_ENABLED must be a boolean")
	}
	cfg.AIIndexEnabled = enabled
	cfg.AIAutoFeatures, err = strconv.ParseBool(value(lookup, "AI_AUTO_FEATURES", "false"))
	if err != nil {
		return errors.New("AI_AUTO_FEATURES must be a boolean")
	}
	cfg.AIPollInterval, err = duration(lookup, "AI_POLL_INTERVAL", 2*time.Second)
	if cfg.AIPollInterval > time.Minute {
		return errors.New("AI_POLL_INTERVAL must not exceed 1m")
	}
	return err
}

func loadOllamaMode(lookup lookupEnv, cfg *Config) error {
	cloud, err := strconv.ParseBool(value(lookup, "OLLAMA_CLOUD", "false"))
	if err != nil {
		return errors.New("OLLAMA_CLOUD must be a boolean")
	}
	cfg.OllamaCloud = cloud
	cfg.OllamaAPIKey = value(lookup, "OLLAMA_API_KEY", "")
	origin := "http://127.0.0.1:11434"
	if cloud {
		origin = "https://ollama.com"
	}
	cfg.OllamaURL, err = ollamaEndpoint(lookup, origin)
	return err
}

func ollamaEndpoint(lookup lookupEnv, fallback string) (string, error) {
	origin, err := nativeOllamaEndpoint(value(lookup, "OLLAMA_URL", ""))
	if err != nil {
		return "", err
	}
	alias, err := nativeOllamaEndpoint(value(lookup, "OLLAMA_BASE_URL", ""))
	if err != nil {
		return "", err
	}
	if origin != "" && alias != "" && origin != alias {
		return "", errors.New("OLLAMA_URL and OLLAMA_BASE_URL must identify the same origin")
	}
	if origin != "" {
		return origin, nil
	}
	if alias != "" {
		return alias, nil
	}
	return fallback, nil
}

func nativeOllamaEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid Ollama endpoint")
	}
	// Accept the common compatibility base path; inference still uses the native chat protocol.
	path := strings.TrimRight(u.Path, "/")
	if u.RawPath == "" && (path == "/v1" || path == "/api" || path == "") {
		u.Path = ""
	}
	return u.String(), nil
}
