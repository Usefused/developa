package openapi

import (
	"reflect"
	"testing"
	"time"
)

type TestBase struct {
	ID string `json:"id"`
}
type testRecord struct {
	TestBase
	Next     *testRecord    `json:"next"`
	Tags     []string       `json:"tags"`
	Counts   map[string]int `json:"counts"`
	Created  time.Time      `json:"created_at"`
	Optional string         `json:"optional,omitempty"`
	Secret   string         `json:"-"`
	private  string
}

func TestSchemaMatchesEmbeddedNullableAndHiddenJSONFields(t *testing.T) {
	registry := NewRegistry()
	ref := registry.Schema(testRecord{})
	if ref["$ref"] != "#/components/schemas/testRecord" {
		t.Fatal(ref)
	}
	schema := registry.Schemas["testRecord"]
	properties := schema["properties"].(map[string]any)
	if len(properties) != 6 {
		t.Fatalf("private or embedded wrapper fields leaked: %#v", properties)
	}
	wantRequired := []string{"id", "next", "tags", "counts", "created_at"}
	if !reflect.DeepEqual(schema["required"], wantRequired) {
		t.Fatal(schema["required"])
	}
	want := map[string]any{
		"id":         Schema{"type": "string"},
		"next":       nullable(ref),
		"tags":       nullable(Schema{"type": "array", "items": Schema{"type": "string"}}),
		"counts":     nullable(Schema{"type": "object", "additionalProperties": Schema{"type": "integer"}}),
		"created_at": Schema{"type": "string", "format": "date-time"},
		"optional":   Schema{"type": "string"},
	}
	if !reflect.DeepEqual(properties, want) {
		t.Fatalf("JSON field shapes differ: %#v", properties)
	}
}

func TestSchemaRejectsUnsupportedEncodingInsteadOfGuessing(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported encoding silently accepted")
		}
	}()
	NewRegistry().Schema(make(chan int))
}
