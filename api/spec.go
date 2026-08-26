// Package api embeds the generated, versioned HTTP contract shipped with the server.
package api

import _ "embed"

//go:embed openapi.json
var document string

func Document() string { return document }
