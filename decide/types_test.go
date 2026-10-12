package decide

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		choices []Option
		valid   bool
	}{
		{"typed", []Option{{Value: Bool(true)}, {Value: String("true")}}, true},
		{"duplicate", []Option{{Value: Bool(true)}, {Value: Bool(true)}}, false},
		{"too few", []Option{{Value: String("a")}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Request{Model: "m", Input: "text", Questions: []Question{{Name: "q", Type: Choice, Instructions: "choose", Choices: tt.choices}}}
			err := r.Validate()
			if (err == nil) != tt.valid || err != nil && !errors.Is(err, ErrInvalidRequest) {
				t.Fatal(err)
			}
		})
	}
}

func TestValueJSON(t *testing.T) {
	for _, raw := range []string{`true`, `false`, `"true"`, `"billing"`, `1`, `null`} {
		t.Run(raw, func(t *testing.T) {
			var v Value
			err := json.Unmarshal([]byte(raw), &v)
			if raw == "1" || raw == "null" {
				if err == nil {
					t.Fatal("accepted invalid choice")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(v)
			if err != nil || string(b) != raw {
				t.Fatalf("%s %v", b, err)
			}
		})
	}
}
