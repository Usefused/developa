package httptransport

import "developa/internal/domain"

// Named wire types keep generated OpenAPI schemas tied to the actual responses.
type InfoResponse struct {
	AuthenticationRequired bool `json:"authentication_required"`
	Configured             bool `json:"configured"`
	WorkspaceManagement    bool `json:"workspace_management,omitempty"`
}

type CapabilitiesResponse struct {
	Calls             bool `json:"calls"`
	Flows             bool `json:"flows"`
	Context           bool `json:"context"`
	Features          bool `json:"features"`
	Answers           bool `json:"answers"`
	AnalysisJobs      bool `json:"analysis_jobs"`
	AutomaticFeatures bool `json:"automatic_features"`
	OllamaConfigured  bool `json:"ollama_configured"`
	OllamaCloud       bool `json:"ollama_cloud"`
	FunctionReviews   bool `json:"function_reviews"`
	ReviewGeneration  bool `json:"function_review_generation"`
}

type StatusResponse struct {
	Status string `json:"status"`
}
type SavedAnswerResponse struct {
	Answer *domain.Answer `json:"answer"`
}
type StreamStarted struct {
	Status  string `json:"status"`
	TraceID string `json:"trace_id"`
}
type StreamError struct {
	Status  int    `json:"status"`
	TraceID string `json:"trace_id"`
}
