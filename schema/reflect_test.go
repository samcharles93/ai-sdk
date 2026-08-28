package schema

import (
	"encoding/json"
	"reflect"
	"testing"
)

func buildMap(t *testing.T, typ reflect.Type) map[string]any {
	t.Helper()
	raw := FromType(typ).Build()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return m
}

func TestFromType_BasicFields(t *testing.T) {
	type In struct {
		Name  string  `json:"name"`
		Count int     `json:"count,omitempty"`
		Rate  float64 `json:"rate"`
		OK    bool    `json:"ok"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	if m["type"] != "object" {
		t.Fatalf("type = %v, want object", m["type"])
	}
	props := m["properties"].(map[string]any)

	nameProp := props["name"].(map[string]any)
	if nameProp["type"] != "string" {
		t.Errorf("name type = %v, want string", nameProp["type"])
	}
	countProp := props["count"].(map[string]any)
	if countProp["type"] != "integer" {
		t.Errorf("count type = %v, want integer", countProp["type"])
	}
	rateProp := props["rate"].(map[string]any)
	if rateProp["type"] != "number" {
		t.Errorf("rate type = %v, want number", rateProp["type"])
	}
	okProp := props["ok"].(map[string]any)
	if okProp["type"] != "boolean" {
		t.Errorf("ok type = %v, want boolean", okProp["type"])
	}

	required, _ := m["required"].([]any)
	reqSet := map[string]bool{}
	for _, r := range required {
		reqSet[r.(string)] = true
	}
	if !reqSet["name"] || !reqSet["rate"] || !reqSet["ok"] {
		t.Errorf("required = %v, want name/rate/ok present", required)
	}
	if reqSet["count"] {
		t.Errorf("count should not be required (omitempty): %v", required)
	}
}

func TestFromType_SkipsDashField(t *testing.T) {
	type In struct {
		Name     string `json:"name"`
		Internal string `json:"-"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	if _, ok := props["Internal"]; ok {
		t.Error("dash-tagged field should be excluded from schema")
	}
	if len(props) != 1 {
		t.Errorf("expected 1 property, got %d", len(props))
	}
}

func TestFromType_PointerFieldIsNullableAndOptional(t *testing.T) {
	type In struct {
		Nickname *string `json:"nickname"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	nick := props["nickname"].(map[string]any)

	types, ok := nick["type"].([]any)
	if !ok || len(types) != 2 || types[0] != "string" || types[1] != "null" {
		t.Errorf("nickname type = %v, want [string null]", nick["type"])
	}

	required, _ := m["required"].([]any)
	for _, r := range required {
		if r.(string) == "nickname" {
			t.Error("pointer field should not be required")
		}
	}
}

func TestFromType_EmbeddedStructFlattened(t *testing.T) {
	type Base struct {
		ID string `json:"id"`
	}
	type In struct {
		Base
		Name string `json:"name"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	if _, ok := props["id"]; !ok {
		t.Error("embedded struct's field 'id' should be flattened into parent properties")
	}
	if _, ok := props["name"]; !ok {
		t.Error("missing own field 'name'")
	}
	if _, ok := props["Base"]; ok {
		t.Error("embedded struct should not appear as its own nested property")
	}

	required, _ := m["required"].([]any)
	reqSet := map[string]bool{}
	for _, r := range required {
		reqSet[r.(string)] = true
	}
	if !reqSet["id"] {
		t.Error("flattened embedded field should still be required")
	}
}

func TestFromType_EnumTag(t *testing.T) {
	type In struct {
		Status string `json:"status" jsonschema:"description=current status,enum=pending|active|done"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	status := props["status"].(map[string]any)

	if status["description"] != "current status" {
		t.Errorf("description = %v, want %q", status["description"], "current status")
	}
	enum, ok := status["enum"].([]any)
	if !ok || len(enum) != 3 {
		t.Fatalf("enum = %v, want 3 values", status["enum"])
	}
	want := []string{"pending", "active", "done"}
	for i, w := range want {
		if enum[i] != w {
			t.Errorf("enum[%d] = %v, want %v", i, enum[i], w)
		}
	}
}

func TestFromType_NumericEnumTag(t *testing.T) {
	type In struct {
		Level int `json:"level" jsonschema:"enum=1|2|3"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	level := props["level"].(map[string]any)
	enum, ok := level["enum"].([]any)
	if !ok || len(enum) != 3 {
		t.Fatalf("enum = %v, want 3 numeric values", level["enum"])
	}
	if enum[0] != 1.0 {
		t.Errorf("enum[0] = %v, want 1", enum[0])
	}
}

func TestFromType_NestedStruct(t *testing.T) {
	type Address struct {
		City string `json:"city"`
	}
	type In struct {
		Home Address `json:"home"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	home := props["home"].(map[string]any)
	if home["type"] != "object" {
		t.Fatalf("home type = %v, want object", home["type"])
	}
	homeProps := home["properties"].(map[string]any)
	if _, ok := homeProps["city"]; !ok {
		t.Error("nested struct schema missing 'city' property")
	}
}

func TestFromType_SliceOfStructs(t *testing.T) {
	type Item struct {
		SKU string `json:"sku"`
	}
	type In struct {
		Items []Item `json:"items"`
	}

	m := buildMap(t, reflect.TypeFor[In]())
	props := m["properties"].(map[string]any)
	items := props["items"].(map[string]any)
	if items["type"] != "array" {
		t.Fatalf("items type = %v, want array", items["type"])
	}
	itemSchema := items["items"].(map[string]any)
	itemProps := itemSchema["properties"].(map[string]any)
	if _, ok := itemProps["sku"]; !ok {
		t.Error("array item schema missing 'sku' property")
	}
}

// Node is a self-referencing type used to prove the recursion guard cuts
// off schema generation instead of looping forever.
type Node struct {
	Value    string `json:"value"`
	Children []Node `json:"children,omitempty"`
}

func TestFromType_RecursiveTypeDoesNotLoop(t *testing.T) {
	// The real assertion is simply that this returns at all: a recursion
	// guard bug would hang or stack-overflow the test process.
	m := buildMap(t, reflect.TypeFor[Node]())

	props := m["properties"].(map[string]any)
	if _, ok := props["value"]; !ok {
		t.Error("recursive type schema missing top-level 'value' property")
	}
	children := props["children"].(map[string]any)
	if children["type"] != "array" {
		t.Fatalf("children type = %v, want array", children["type"])
	}
	// The recursion guard cuts off at the second level: the item schema
	// for children is a bare object, not another full Node schema with
	// its own nested children.
	itemSchema := children["items"].(map[string]any)
	if itemSchema["type"] != "object" {
		t.Errorf("recursive item schema type = %v, want object", itemSchema["type"])
	}
	if _, ok := itemSchema["properties"]; ok {
		t.Error("recursion guard should stop before expanding nested Node properties again")
	}
}
