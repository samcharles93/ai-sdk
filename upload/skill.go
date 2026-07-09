package upload

import (
	"encoding/json"
	"errors"
)

// Skill represents a simple skill definition uploaded by the UI.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Template    string `json:"template"`
}

// ParseSkill attempts to parse a skill definition from JSON data. The
// format is intentionally small and JSON-only to avoid adding dependencies
// for YAML parsing. If YAML support is required later, the caller may
// convert before calling into this package.
func ParseSkill(data []byte) (*Skill, error) {
	if len(data) == 0 {
		return nil, errors.New("empty skill data")
	}
	var s Skill
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
