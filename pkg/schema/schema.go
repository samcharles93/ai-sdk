package schema

import "encoding/json"

// JSONSchema is a builder for constructing JSON Schema objects that are
// compatible with AI model tool definitions and structured outputs.
type JSONSchema struct {
	schema map[string]any
}

// NewJSONSchema creates a new JSONSchema builder initialised with the
// given type.
func NewJSONSchema(schemaType string) *JSONSchema {
	return &JSONSchema{
		schema: map[string]any{
			"type": schemaType,
		},
	}
}

// Object creates a JSONSchema builder for an object type.
func Object() *JSONSchema {
	return NewJSONSchema("object")
}

// Array creates a JSONSchema builder for an array type.
func Array() *JSONSchema {
	return NewJSONSchema("array")
}

// StringProp adds a string property to an object schema.
func (s *JSONSchema) StringProp(name, description string) *JSONSchema {
	s.addProperty(name, map[string]any{
		"type":        "string",
		"description": description,
	})
	return s
}

// NumberProp adds a number property to an object schema.
func (s *JSONSchema) NumberProp(name, description string) *JSONSchema {
	s.addProperty(name, map[string]any{
		"type":        "number",
		"description": description,
	})
	return s
}

// BoolProp adds a boolean property to an object schema.
func (s *JSONSchema) BoolProp(name, description string) *JSONSchema {
	s.addProperty(name, map[string]any{
		"type":        "boolean",
		"description": description,
	})
	return s
}

func (s *JSONSchema) addProperty(name string, prop map[string]any) {
	props, _ := s.schema["properties"].(map[string]any)
	if props == nil {
		props = make(map[string]any)
		s.schema["properties"] = props
	}
	props[name] = prop
}

// Required marks the named properties as required.
func (s *JSONSchema) Required(names ...string) *JSONSchema {
	s.schema["required"] = names
	return s
}

// Items sets the items schema for an array type.
func (s *JSONSchema) Items(item *JSONSchema) *JSONSchema {
	s.schema["items"] = item.schema
	return s
}

// Description sets the schema description.
func (s *JSONSchema) Description(desc string) *JSONSchema {
	s.schema["description"] = desc
	return s
}

// Build returns the schema as a json.RawMessage suitable for use in tool
// definitions and structured output specifications.
func (s *JSONSchema) Build() json.RawMessage {
	b, _ := json.Marshal(s.schema)
	return b
}
