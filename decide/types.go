package decide

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Value retains the distinction between boolean and string choices.
// Its zero value is the empty string.
type Value struct {
	text    string
	boolean *bool
}

func String(v string) Value                 { return Value{text: v} }
func Bool(v bool) Value                     { return Value{boolean: &v} }
func (v Value) StringValue() (string, bool) { return v.text, v.boolean == nil }
func (v Value) BoolValue() (bool, bool) {
	if v.boolean == nil {
		return false, false
	}
	return *v.boolean, true
}

func (v Value) MarshalJSON() ([]byte, error) {
	if v.boolean != nil {
		return json.Marshal(*v.boolean)
	}
	return json.Marshal(v.text)
}

func (v *Value) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	switch x := raw.(type) {
	case string:
		*v = String(x)
	case bool:
		*v = Bool(x)
	default:
		return fmt.Errorf("%w: choice must be string or bool", ErrInvalidRequest)
	}
	return nil
}

type Kind string

const (
	Predicate Kind = "predicate"
	Choice    Kind = "choice"
	Score     Kind = "score"
	Refusal   Kind = "refusal"
)

type (
	Option struct {
		Value       Value  `json:"value"`
		Description string `json:"description,omitempty"`
	}
	Level struct {
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	}
	Question struct {
		Name         string   `json:"name"`
		Type         Kind     `json:"type"`
		Instructions string   `json:"instructions"`
		Choices      []Option `json:"choices,omitempty"`
		Levels       []Level  `json:"levels,omitempty"`
	}
)

// Message contains user evidence. Images are data URLs or public HTTP(S) URLs.
type (
	Message struct {
		Text   string
		Images []string
	}
	Request struct {
		Model           string
		Input           string
		Messages        []Message
		Questions       []Question
		ProviderOptions map[string]any
	}
)

type (
	ChoiceProbability struct {
		Value       Value   `json:"value"`
		Probability float64 `json:"probability"`
	}
	ScoreProbability struct {
		Label       string  `json:"label"`
		Value       int     `json:"value"`
		Probability float64 `json:"probability"`
	}
	PredicateAnswer struct {
		Probability float64 `json:"probability"`
	}
	ChoiceAnswer struct {
		Choice        Value               `json:"choice"`
		Probabilities []ChoiceProbability `json:"probabilities"`
	}
	ScoreAnswer struct {
		Score         float64            `json:"score"`
		Probabilities []ScoreProbability `json:"probabilities"`
	}
	RefusalAnswer struct{}
	// Answer is a tagged union: exactly one payload matches Type. Confidence for a
	// predicate is the probability of the likelier boolean outcome; refusal is zero.
	Answer struct {
		Name       string           `json:"name"`
		Type       Kind             `json:"type"`
		Confidence float64          `json:"confidence"`
		Predicate  *PredicateAnswer `json:"predicate,omitempty"`
		Choice     *ChoiceAnswer    `json:"choice,omitempty"`
		Score      *ScoreAnswer     `json:"score,omitempty"`
		Refusal    *RefusalAnswer   `json:"refusal,omitempty"`
	}
)

func (a Answer) IsRefusal() bool { return a.Type == Refusal }

type Usage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails struct {
		CacheWriteTokens int `json:"cache_write_tokens"`
		CachedTokens     int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}
type Response struct {
	Model   string   `json:"model"`
	Answers []Answer `json:"answers"`
	Usage   Usage    `json:"usage"`
}

func (r Response) Answer(name string) (Answer, bool) {
	for _, a := range r.Answers {
		if a.Name == name {
			return a, true
		}
	}
	return Answer{}, false
}

func (r Request) Validate() error {
	bad := func(s string) error { return fmt.Errorf("%w: %s", ErrInvalidRequest, s) }
	if r.Model == "" || (r.Input == "" && len(r.Messages) == 0) || (r.Input != "" && len(r.Messages) != 0) || len(r.Questions) == 0 {
		return bad("model, one input form and questions required")
	}
	images := 0
	for _, m := range r.Messages {
		if m.Text == "" && len(m.Images) == 0 {
			return bad("empty message")
		}
		for _, u := range m.Images {
			images++
			if !strings.HasPrefix(u, "data:image/") && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
				return bad("invalid image URL")
			}
		}
	}
	if images > 128 {
		return bad("at most 128 images")
	}
	names := map[string]bool{}
	for _, q := range r.Questions {
		if q.Name == "" || names[q.Name] || q.Instructions == "" {
			return bad("unique names and instructions required")
		}
		names[q.Name] = true
		switch q.Type {
		case Predicate:
			if len(q.Choices) != 0 || len(q.Levels) != 0 {
				return bad("predicate cannot have choices or levels")
			}
		case Choice:
			if len(q.Choices) < 2 || len(q.Choices) > 255 || len(q.Levels) != 0 {
				return bad("choice requires 2 to 255 choices")
			}
			seen := map[string]bool{}
			for _, c := range q.Choices {
				b, _ := json.Marshal(c.Value)
				if seen[string(b)] {
					return bad("duplicate choice")
				}
				seen[string(b)] = true
			}
		case Score:
			if len(q.Levels) < 2 || len(q.Choices) != 0 {
				return bad("score requires levels")
			}
			seen := map[string]bool{}
			for _, l := range q.Levels {
				if l.Label == "" || seen[l.Label] {
					return bad("unique score labels required")
				}
				seen[l.Label] = true
			}
		default:
			return bad("unknown question type")
		}
	}
	return nil
}
