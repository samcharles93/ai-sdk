package schema

import (
	"reflect"
	"strconv"
	"strings"
	"time"
)

// FromType derives a JSON Schema for t via reflection, reading `json` tags
// for property naming/omission and `jsonschema` tags for description and
// enum constraints. It is the Go equivalent of the AI SDK's automatic
// Zod-to-JSON-Schema conversion for typed tool inputs.
//
// Supported `jsonschema` tag keys (comma-separated key=value pairs):
//
//	jsonschema:"description=what this field is,enum=a|b|c"
//
// A field is required unless its `json` tag carries `omitempty` or the
// field's Go type is a pointer. Anonymous (embedded) struct fields are
// flattened into the parent object, matching how encoding/json treats
// them. Self-referencing types are cut off at the point of recursion to
// avoid an infinite schema.
func FromType(t reflect.Type) *JSONSchema {
	return fromType(t, map[reflect.Type]bool{})
}

func fromType(t reflect.Type, visiting map[reflect.Type]bool) *JSONSchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == reflect.TypeFor[time.Time]() {
		return NewJSONSchema("string")
	}

	switch t.Kind() {
	case reflect.Struct:
		if visiting[t] {
			// Recursion guard: stop descending into a type we're already
			// building a schema for, rather than recursing forever.
			return NewJSONSchema("object")
		}
		visiting[t] = true
		defer delete(visiting, t)
		return structSchema(t, visiting)
	case reflect.Slice, reflect.Array:
		elem := fromType(t.Elem(), visiting)
		return Array().Items(elem)
	case reflect.Map:
		s := NewJSONSchema("object")
		s.schema["additionalProperties"] = fromType(t.Elem(), visiting).schema
		return s
	case reflect.String:
		return NewJSONSchema("string")
	case reflect.Bool:
		return NewJSONSchema("boolean")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return NewJSONSchema("integer")
	case reflect.Float32, reflect.Float64:
		return NewJSONSchema("number")
	default:
		return NewJSONSchema("object")
	}
}

func structSchema(t reflect.Type, visiting map[reflect.Type]bool) *JSONSchema {
	obj := Object()
	var required []string

	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		name, opts := parseJSONTag(jsonTag)

		if field.Anonymous && name == "" {
			embedded := fromType(field.Type, visiting)
			for k, v := range asProperties(embedded) {
				if prop, ok := v.(map[string]any); ok {
					obj.addProperty(k, prop)
				}
			}
			required = append(required, embeddedRequired(embedded)...)
			continue
		}

		if name == "" {
			name = field.Name
		}

		fieldSchema := fromType(field.Type, visiting)
		nullable := field.Type.Kind() == reflect.Pointer

		desc, enum := parseJSONSchemaTag(field.Tag.Get("jsonschema"))
		if desc != "" {
			fieldSchema.Description(desc)
		}
		if len(enum) > 0 {
			fieldSchema.schema["enum"] = enum
		}
		if nullable {
			if typ, ok := fieldSchema.schema["type"]; ok {
				fieldSchema.schema["type"] = []any{typ, "null"}
			}
		}

		obj.addProperty(name, fieldSchema.schema)

		if !opts["omitempty"] && !nullable {
			required = append(required, name)
		}
	}

	if len(required) > 0 {
		obj.Required(required...)
	}
	return obj
}

func asProperties(s *JSONSchema) map[string]any {
	props, _ := s.schema["properties"].(map[string]any)
	return props
}

func embeddedRequired(s *JSONSchema) []string {
	req, _ := s.schema["required"].([]string)
	return req
}

// parseJSONTag splits a `json:"name,omitempty"`-style tag into the
// property name and its option set.
func parseJSONTag(tag string) (name string, opts map[string]bool) {
	opts = map[string]bool{}
	if tag == "" {
		return "", opts
	}
	first := true
	for part := range strings.SplitSeq(tag, ",") {
		if first {
			name = part
			first = false
			continue
		}
		opts[part] = true
	}
	return name, opts
}

// parseJSONSchemaTag parses a `jsonschema:"description=...,enum=a|b|c"`
// tag into its description and enum values.
func parseJSONSchemaTag(tag string) (description string, enum []any) {
	if tag == "" {
		return "", nil
	}
	for kv := range strings.SplitSeq(tag, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "description":
			description = v
		case "enum":
			for e := range strings.SplitSeq(v, "|") {
				enum = append(enum, enumValue(e))
			}
		}
	}
	return description, enum
}

// enumValue keeps enum tag values as strings unless they parse cleanly as
// numbers, so `enum=1|2|3` produces a numeric enum rather than a string
// one.
func enumValue(raw string) any {
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		return n
	}
	return raw
}
