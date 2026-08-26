package httptransport

import (
	"maps"
	"strings"

	"developa/internal/domain"
	"developa/internal/openapi"
)

func openAPIRequests(registry *openapi.Registry) map[string]apiSchema {
	flowRequestSchema(registry)
	return map[string]apiSchema{
		"answer":         requestBody(answerRequestSchema(registry), true, 16<<10),
		"review":         requestBody(reviewRequestSchema(registry), true, 2048),
		"workspace":      requestBody(workspaceRequestSchema(registry), true, 8192),
		"repositoryPath": requestBody(repositoryPathRequestSchema(registry), true, 8192),
		"empty":          requestBody(apiSchema{"type": []string{"object", "null"}, "additionalProperties": false}, false, 1024),
	}
}

func requestBody(schema apiSchema, required bool, limit int) apiSchema {
	return apiSchema{"required": required, "description": "JSON only; unknown fields and trailing JSON values are rejected. Content-Type may be omitted. The encoded body is byte-limited independently of decoded field limits.", "x-max-body-bytes": limit, "content": apiSchema{"application/json": apiSchema{"schema": schema}}}
}

func inputSchema(registry *openapi.Registry, value any, name string) (apiSchema, map[string]any, apiSchema) {
	ref := registry.Schema(value)["$ref"].(string)
	schema := maps.Clone(registry.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")])
	properties := maps.Clone(schema["properties"].(map[string]any))
	schema["properties"] = properties
	// Decoders accept omitted zero values even when response encoders always emit them.
	delete(schema, "required")
	registry.Schemas[name] = schema
	return schema, properties, apiSchema{"$ref": "#/components/schemas/" + name}
}

func optionalIDSchema() apiSchema { return apiSchema{"type": "string", "pattern": "^([a-f0-9]{64})?$"} }

func limitedTextSchema(limit int) apiSchema {
	return apiSchema{"type": "string", "maxLength": limit, "pattern": "^[^\\u0000]*$", "x-max-utf8-bytes": limit, "description": "Server limits decoded UTF-8 bytes, not characters; NUL is rejected."}
}

func exclusiveIDs(first, second string) apiSchema {
	return apiSchema{"not": apiSchema{"required": []string{first, second}, "properties": apiSchema{first: apiSchema{"minLength": 1}, second: apiSchema{"minLength": 1}}}}
}

func flowRequestSchema(registry *openapi.Registry) {
	schema, properties, _ := inputSchema(registry, domain.FlowOptions{}, "FlowRequest")
	properties["symbol_id"], properties["feature_id"] = optionalIDSchema(), optionalIDSchema()
	properties["depth"] = apiSchema{"type": "integer", "minimum": 0, "maximum": 12, "default": 6, "description": "Omitted or zero uses six."}
	properties["limit"] = apiSchema{"type": "integer", "minimum": 0, "maximum": 100, "default": 80, "description": "Omitted or zero uses eighty."}
	schema["allOf"] = []apiSchema{exclusiveIDs("symbol_id", "feature_id")}
}

func answerRequestSchema(registry *openapi.Registry) apiSchema {
	schema, properties, ref := inputSchema(registry, domain.AnswerRequest{}, "AnswerRequest")
	schema["required"] = []string{"question"}
	question := limitedTextSchema(2000)
	question["minLength"], question["allOf"] = 1, []apiSchema{{"pattern": "\\S"}}
	properties["question"] = question
	properties["symbol_id"], properties["feature_id"] = optionalIDSchema(), optionalIDSchema()
	properties["flow"] = apiSchema{"anyOf": []apiSchema{{"$ref": "#/components/schemas/FlowRequest"}, {"type": "null"}}}
	schema["allOf"] = []apiSchema{exclusiveIDs("symbol_id", "feature_id"), exclusiveFlowTarget("symbol_id"), exclusiveFlowTarget("feature_id")}
	return ref
}

func exclusiveFlowTarget(name string) apiSchema {
	return apiSchema{"not": apiSchema{"required": []string{name, "flow"}, "properties": apiSchema{name: apiSchema{"minLength": 1}, "flow": apiSchema{"type": "object"}}}}
}

func reviewRequestSchema(registry *openapi.Registry) apiSchema {
	schema, properties, ref := inputSchema(registry, domain.ReviewOptions{}, "ReviewRequest")
	properties["symbol_id"], properties["callee_of"] = optionalIDSchema(), optionalIDSchema()
	properties["limit"] = apiSchema{"type": "integer", "minimum": 0, "maximum": 8, "default": 4, "description": "Omitted or zero uses four."}
	properties["offset"] = apiSchema{"type": "integer", "minimum": 0, "maximum": 100000, "default": 0}
	schema["allOf"] = []apiSchema{exclusiveIDs("symbol_id", "callee_of")}
	return ref
}

func workspaceRequestSchema(registry *openapi.Registry) apiSchema {
	schema, properties, ref := inputSchema(registry, domain.AddWorkspaceRequest{}, "AddWorkspaceRequest")
	schema["required"] = []string{"root_id", "path"}
	properties["root_id"] = apiSchema{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	path := limitedTextSchema(4096)
	path["minLength"], path["description"] = 1, "Folder within an allowed root, using . for that root. Absolute paths and traversal are rejected."
	properties["path"], properties["name"] = path, limitedTextSchema(200)
	return ref
}

func repositoryPathRequestSchema(registry *openapi.Registry) apiSchema {
	schema, properties, ref := inputSchema(registry, domain.ResolveRepositoryRequest{}, "ResolveRepositoryRequest")
	schema["required"] = []string{"path"}
	path := limitedTextSchema(4096)
	path["minLength"] = 1
	path["description"] = "Absolute repository-root path in the Denverr process filesystem."
	properties["path"] = path
	return ref
}
