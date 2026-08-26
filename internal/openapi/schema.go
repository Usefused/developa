// Package openapi derives JSON Schema shapes from the Go values written by the API.
package openapi

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

type Schema = map[string]any

type Registry struct{ Schemas map[string]Schema }

func NewRegistry() *Registry { return &Registry{Schemas: map[string]Schema{}} }

func (r *Registry) Schema(value any) Schema { return r.schema(reflect.TypeOf(value)) }

func (r *Registry) schema(t reflect.Type) Schema {
	if t == reflect.TypeFor[time.Time]() {
		return Schema{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return nullable(r.schema(t.Elem()))
	case reflect.Struct:
		return r.structure(t)
	case reflect.Slice:
		return nullable(Schema{"type": "array", "items": r.schema(t.Elem())})
	case reflect.Map:
		return nullable(Schema{"type": "object", "additionalProperties": r.schema(t.Elem())})
	case reflect.String:
		return Schema{"type": "string"}
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.Int, reflect.Int32, reflect.Int64:
		return Schema{"type": "integer"}
	default:
		panic(fmt.Sprintf("OpenAPI schema needs an explicit encoding for %s", t))
	}
}

func nullable(schema Schema) Schema { return Schema{"anyOf": []Schema{schema, {"type": "null"}}} }

func (r *Registry) structure(t reflect.Type) Schema {
	name := t.Name()
	if name == "" {
		return r.object(t)
	}
	ref := Schema{"$ref": "#/components/schemas/" + name}
	if _, exists := r.Schemas[name]; exists {
		return ref
	}
	// Reserve the name before descending so recursive DTOs remain references.
	r.Schemas[name] = Schema{}
	r.Schemas[name] = r.object(t)
	return ref
}

func (r *Registry) object(t reflect.Type) Schema {
	properties, required := map[string]any{}, []string{}
	r.fields(t, properties, &required)
	return Schema{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func (r *Registry) fields(t reflect.Type, properties map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" || !field.IsExported() {
			continue
		}
		if field.Anonymous && name == "" {
			r.fields(field.Type, properties, required)
			continue
		}
		if name == "" {
			name = field.Name
		}
		properties[name] = r.schema(field.Type)
		if !strings.Contains(options, "omitempty") {
			*required = append(*required, name)
		}
	}
}
