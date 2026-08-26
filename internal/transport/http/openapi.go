package httptransport

import (
	"encoding/json"
	"net/http"
	"strings"

	"developa/internal/openapi"
)

type apiSchema = openapi.Schema

type apiEndpoint struct {
	Method, Path, ID, Summary, Description string
	Response                               any
	Parameters                             []apiSchema
	Request                                string
	Status                                 int
	Public, Stream                         bool
	Events                                 map[string]any
	Errors                                 []int
}

// OpenAPIDocument is generated offline; serving it never consults a repository or model.
func OpenAPIDocument() ([]byte, error) {
	registry := openapi.NewRegistry()
	requests := openAPIRequests(registry)
	paths := map[string]any{}
	for _, endpoint := range openAPIEndpoints() {
		addOpenAPIEndpoint(paths, registry, requests, endpoint, false)
	}
	for _, endpoint := range repositoryAPIEndpoints() {
		endpoint.Errors = append(endpoint.Errors, http.StatusNotFound)
		scoped := endpoint
		scoped.Path = "/api/repositories/{repository}" + endpoint.Path
		scoped.Parameters = append([]apiSchema{pathParameter("repository")}, endpoint.Parameters...)
		addOpenAPIEndpoint(paths, registry, requests, scoped, false)
		endpoint.Path = "/api" + endpoint.Path
		addOpenAPIEndpoint(paths, registry, requests, endpoint, true)
	}
	document := apiSchema{"openapi": "3.1.0", "info": apiSchema{"title": "Denverr API", "version": "0.3.0", "description": apiDescription},
		"servers": []apiSchema{{"url": "/", "description": "This Denverr engine"}}, "security": []apiSchema{{"bearerAuth": []string{}}},
		"paths": paths, "components": apiSchema{"schemas": registry.Schemas, "securitySchemes": apiSchema{"bearerAuth": apiSchema{"type": "http", "scheme": "bearer", "description": "Shared operator token (DENVERR_API_TOKEN), not individual user or per-repository authorization."}}},
		"x-generated-by": "go run ./cmd/openapi; do not edit api/openapi.json by hand"}
	data, err := json.MarshalIndent(document, "", "  ")
	return append(data, '\n'), err
}

const apiDescription = "Implemented API only. Workspace registration persists in PostgreSQL. Source reads are repository-scoped and pinned to immutable publication snapshots. Positions use one-based lines and UTF-8 byte columns; span ends are exclusive. Static call flows are not runtime execution order. AI claims are inferred and cite bounded source evidence. GET and saved-answer lookup do not invoke inference. Explicit AI requests use operator-configured Ollama; cloud requires opt-in. No MCP, clone/fetch, dependency-version, multi-tenant, or general reference endpoints are implemented. Browser mutations reject foreign Origin/Sec-Fetch-Site; no CORS is enabled."

func addOpenAPIEndpoint(paths map[string]any, registry *openapi.Registry, requests map[string]apiSchema, endpoint apiEndpoint, alias bool) {
	operation := apiSchema{"operationId": endpoint.ID, "summary": endpoint.Summary, "description": endpoint.Description, "responses": openAPIResponses(registry, endpoint)}
	if alias {
		operation["operationId"] = "default" + strings.ToUpper(endpoint.ID[:1]) + endpoint.ID[1:]
		operation["description"] = "Compatibility alias for the engine's first workspace. Prefer repository-qualified paths for agents. " + endpoint.Description
		operation["x-default-workspace"] = true
	}
	if endpoint.Public {
		operation["security"] = []any{}
	}
	if len(endpoint.Parameters) > 0 {
		operation["parameters"] = endpoint.Parameters
	}
	if endpoint.Request != "" {
		operation["requestBody"] = requests[endpoint.Request]
	}
	if endpoint.Stream {
		operation["x-sse-events"] = eventSchemas(registry, endpoint.Events)
	}
	item, exists := paths[endpoint.Path].(apiSchema)
	if !exists {
		item = apiSchema{}
		paths[endpoint.Path] = item
	}
	item[strings.ToLower(endpoint.Method)] = operation
}

func openAPIResponses(registry *openapi.Registry, endpoint apiEndpoint) apiSchema {
	status := endpoint.Status
	if status == 0 {
		status = http.StatusOK
	}
	responses := apiSchema{statusKey(status): successResponse(registry, endpoint), "default": jsonResponse("Safe error code. No source, credentials, SQL, or internal error text is returned.", registry.Schema(StatusResponse{}))}
	for _, code := range responseErrors(endpoint) {
		responses[statusKey(code)] = jsonResponse(http.StatusText(code), registry.Schema(StatusResponse{}))
	}
	if status == http.StatusCreated {
		responses["200"] = jsonResponse("Workspace already registered; no duplicate tracker is created.", registry.Schema(endpoint.Response))
	}
	return responses
}

func responseErrors(endpoint apiEndpoint) []int {
	codes := append([]int{}, endpoint.Errors...)
	if !endpoint.Public {
		codes = append(codes, 401, 503, 504)
	}
	if len(endpoint.Parameters) > 0 || endpoint.Request != "" {
		codes = append(codes, 400)
	}
	if endpoint.Method == "POST" {
		codes = append(codes, 403)
	}
	return codes
}

func successResponse(registry *openapi.Registry, endpoint apiEndpoint) apiSchema {
	if endpoint.Stream {
		return apiSchema{"description": "UTF-8 SSE frames: event name plus a JSON data line. Comment heartbeats every 15s. Results are persisted before delivery; this is not token streaming. Errors after headers use the error event. No durable replay or Last-Event-ID support.", "headers": traceHeaders(), "content": apiSchema{"text/event-stream": apiSchema{"schema": apiSchema{"type": "string"}}}}
	}
	if endpoint.Response == nil {
		return jsonResponse("The generated OpenAPI document.", apiSchema{"type": "object", "additionalProperties": true})
	}
	return jsonResponse("Success", registry.Schema(endpoint.Response))
}

func eventSchemas(registry *openapi.Registry, events map[string]any) apiSchema {
	result := apiSchema{"error": registry.Schema(StreamError{})}
	for name, value := range events {
		result[name] = registry.Schema(value)
	}
	return result
}

func jsonResponse(description string, schema apiSchema) apiSchema {
	return apiSchema{"description": description, "headers": traceHeaders(), "content": apiSchema{"application/json": apiSchema{"schema": schema}}}
}

func traceHeaders() apiSchema {
	return apiSchema{"X-Trace-ID": apiSchema{"description": "Safe OpenTelemetry trace correlation.", "schema": apiSchema{"type": "string"}}, "Cache-Control": apiSchema{"schema": apiSchema{"type": "string", "const": "no-store"}}}
}
