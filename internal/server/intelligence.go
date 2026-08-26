package server

import (
	"developa/internal/application"
	"developa/internal/config"
	"developa/internal/domain"
	"developa/internal/model/ollama"
	"developa/internal/store/postgres"
	httptransport "developa/internal/transport/http"
)

func repositoryExplorer(store *postgres.Store, manager *application.Manager, cfg config.Config, admission *application.AnalysisAdmission) (*httptransport.Explorer, *application.AnalysisWorker, error) {
	repo := manager.Repository().ID
	answers, err := intelligenceService(store, repo, cfg, cfg.OllamaAnswerModel)
	if err != nil {
		return nil, nil, err
	}
	analysis, err := intelligenceService(store, repo, cfg, cfg.OllamaAnalysisModel)
	if err != nil {
		return nil, nil, err
	}
	worker, err := analysisWorker(store, analysis, repo, cfg, admission)
	if err != nil {
		return nil, nil, err
	}
	reviewer, _ := analysis.(domain.FunctionReviewer)
	return &httptransport.Explorer{Catalog: store, Tracker: manager, RepositoryID: repo, Token: cfg.APIKey, Knowledge: store,
		Intelligence: answers, Reviewer: reviewer, OllamaCloud: cfg.OllamaCloud, Jobs: worker, AutomaticFeatures: cfg.AIAutoFeatures}, worker, nil
}

func analysisWorker(store domain.AnalysisJobStore, intelligence domain.Intelligence, repo string, cfg config.Config, admission *application.AnalysisAdmission) (*application.AnalysisWorker, error) {
	if !cfg.AIIndexEnabled {
		intelligence = nil
	}
	return application.NewAnalysisWorker(store, intelligence, application.AnalysisWorkerConfig{
		RepositoryID: repo, PollInterval: cfg.AIPollInterval, ExecutionTimeout: cfg.AITimeout, Admission: admission,
	})
}

func intelligenceService(store domain.IntelligenceStore, repo string, cfg config.Config, modelName string) (domain.Intelligence, error) {
	if repo == "" {
		return nil, nil
	}
	var model application.StructuredModel
	if modelName != "" {
		client, err := ollama.New(ollama.Config{BaseURL: cfg.OllamaURL, Model: modelName, Timeout: cfg.OllamaTimeout, Cloud: cfg.OllamaCloud, APIKey: cfg.OllamaAPIKey})
		if err != nil {
			return nil, err
		}
		model = client
	}
	// A single model batch is a durable checkpoint; the worker schedules the next page independently.
	return application.NewIntelligence(store, model, application.IntelligenceConfig{RepositoryID: repo, Timeout: cfg.AITimeout, MaxModelCalls: 1})
}
