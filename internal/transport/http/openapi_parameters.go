package httptransport

import (
	"strconv"

	"developa/internal/domain"
)

func statusKey(status int) string { return strconv.Itoa(status) }

func pathParameter(name string) apiSchema {
	return apiSchema{"name": name, "in": "path", "required": true, "schema": apiSchema{"type": "string", "pattern": "^[a-f0-9]{64}$"}}
}

func queryParameter(name string, schema apiSchema) apiSchema {
	return apiSchema{"name": name, "in": "query", "schema": schema}
}

func integerParameter(name string, fallback, min, max int) apiSchema {
	return queryParameter(name, apiSchema{"type": "integer", "minimum": min, "maximum": max, "default": fallback})
}

func textParameter(name string, limit int) apiSchema {
	return queryParameter(name, apiSchema{"type": "string", "maxLength": limit, "x-max-utf8-bytes": limit, "description": "UTF-8; NUL is rejected. Length is bounded in bytes by the server."})
}

func idParameter(name string) apiSchema {
	return queryParameter(name, apiSchema{"type": "string", "pattern": "^[a-f0-9]{64}$"})
}
func enumParameter(name string, values []string, fallback string) apiSchema {
	return queryParameter(name, apiSchema{"type": "string", "enum": values, "default": fallback})
}

func pageParameters(limit int) []apiSchema {
	return []apiSchema{textParameter("q", 200), integerParameter("limit", limit, 1, 100), integerParameter("offset", 0, 0, 100000)}
}

func catalogParameters(limit int) []apiSchema {
	return append(pageParameters(limit), enumParameter("kind", []string{"", "function", "method", "struct", "interface", "interface_method", "alias", "named_type", "field", "constant", "variable", "closure"}, ""), textParameter("file", 4096))
}

func callParameters() []apiSchema {
	return []apiSchema{idParameter("symbol_id"), enumParameter("direction", []string{"in", "out"}, "out"), enumParameter("resolution", []string{"", "resolved", "unresolved", "external", "builtin"}, ""), integerParameter("limit", 50, 1, 100), integerParameter("offset", 0, 0, 100000)}
}

func chainParameters() []apiSchema {
	return []apiSchema{pathParameter("symbol"), enumParameter("direction", []string{"in", "out"}, "out"), integerParameter("depth", 2, 1, 5), integerParameter("limit", 40, 1, 100)}
}

func implementationParameters() []apiSchema {
	return []apiSchema{pathParameter("symbol"), integerParameter("limit", 20, 1, 100), integerParameter("offset", 0, 0, 100000)}
}

func sourceParameters() []apiSchema {
	offset := queryParameter("offset", apiSchema{"type": "integer", "minimum": 0, "default": 0, "description": "UTF-8 byte offset relative to the declaration; must be a rune boundary no greater than total_bytes."})
	return []apiSchema{pathParameter("symbol"), integerParameter("limit", domain.DefaultSourceLimit, domain.MinSourceLimit, domain.MaxSourceLimit), offset}
}

func flowParameters() []apiSchema {
	return []apiSchema{idParameter("symbol_id"), idParameter("feature_id"), integerParameter("depth", 6, 1, 12), integerParameter("limit", 80, 1, 100)}
}
func reviewParameters() []apiSchema {
	return []apiSchema{idParameter("symbol_id"), idParameter("callee_of"), integerParameter("limit", 4, 1, 8), integerParameter("offset", 0, 0, 100000)}
}

func folderParameters() []apiSchema {
	root, path := idParameter("root_id"), textParameter("path", 4096)
	root["required"], path["required"] = true, true
	path["description"] = "Relative folder returned by the browser API; use . for the allowed root. Absolute and traversal paths are rejected."
	return []apiSchema{root, path, integerParameter("offset", 0, 0, 100000)}
}
