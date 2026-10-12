package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/decide"
)

func TestDecideWire(t *testing.T) {
	for _, messages := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "images"}[messages], func(t *testing.T) {
			req := decide.Request{Model: "model", Input: "evidence", Questions: []decide.Question{
				{Name: "p", Type: decide.Predicate, Instructions: "predicate"},
				{Name: "c", Type: decide.Choice, Instructions: "choose", Choices: []decide.Option{{Value: decide.Bool(true)}, {Value: decide.String("true")}}},
				{Name: "s", Type: decide.Score, Instructions: "score", Levels: []decide.Level{{Label: "low"}, {Label: "high"}}},
				{Name: "r", Type: decide.Predicate, Instructions: "refuse"},
			}, ProviderOptions: map[string]any{"openai": DecideOptions{SafetyIdentifier: "user"}}}
			if messages {
				req.Input = ""
				req.Messages = []decide.Message{{Text: "evidence", Images: []string{"https://example.com/a.png", "data:image/png;base64,AA=="}}}
			}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/decisions" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test" {
					t.Errorf("bad request %s %s", r.Method, r.URL)
				}
				var body struct {
					Input     json.RawMessage   `json:"input"`
					Questions []decide.Question `json:"questions"`
					Safety    string            `json:"safety_identifier"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body.Questions) != 4 || body.Questions[0].Name != "p" || body.Questions[1].Name != "c" || body.Questions[2].Name != "s" || body.Questions[3].Name != "r" || body.Safety != "user" {
					t.Errorf("bad body %+v", body)
				}
				b, _ := json.Marshal(body.Questions[1].Choices)
				if string(b) != `[{"value":true},{"value":"true"}]` {
					t.Errorf("typed choices %s", b)
				}
				if messages {
					var ms []struct {
						Role    string
						Content []map[string]string
					}
					if err := json.Unmarshal(body.Input, &ms); err != nil {
						t.Fatal(err)
					}
					if len(ms) != 1 || ms[0].Role != "user" || len(ms[0].Content) != 3 || ms[0].Content[0]["type"] != "input_text" || ms[0].Content[1]["image_url"] != "https://example.com/a.png" || ms[0].Content[2]["type"] != "input_image" {
						t.Errorf("messages %+v", ms)
					}
				} else if string(body.Input) != `"evidence"` {
					t.Errorf("input %s", body.Input)
				}
				_, _ = w.Write([]byte(`{"model":"model","answers":[{"name":"p","type":"predicate","probability":0.9},{"name":"c","type":"choice","choice":true,"confidence":0.8,"probabilities":[{"value":true,"probability":0.9},{"value":"true","probability":0.1}]},{"name":"s","type":"score","score":0.8,"confidence":0.6,"probabilities":[{"label":"low","value":0,"probability":0.2},{"label":"high","value":1,"probability":0.8}]},{"name":"r","type":"refusal"}],"usage":{"input_tokens":12,"output_tokens":0,"total_tokens":12,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":0}}}`))
			}))
			defer ts.Close()
			p, err := New(Config{BaseURL: ts.URL, APIKey: "test"})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.Decide(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Answers) != 4 || out.Answers[0].Predicate.Probability != 0.9 || out.Answers[2].Score.Score != 0.8 || out.Answers[2].Score.Probabilities[1].Label != "high" || out.Usage.InputTokensDetails.CachedTokens != 2 || out.Usage.TotalTokens != 12 {
				t.Fatalf("out %+v", out)
			}
			a, ok := out.Answer("c")
			if !ok || a.Confidence != 0.8 {
				t.Fatal(a)
			}
			v, ok := a.Choice.Choice.BoolValue()
			if !ok || !v {
				t.Fatal(a)
			}
			s, ok := a.Choice.Probabilities[1].Value.StringValue()
			if !ok || s != "true" {
				t.Fatal(a)
			}
			a, ok = out.Answer("r")
			if !ok || !a.IsRefusal() || a.Refusal == nil {
				t.Fatal(a)
			}
		})
	}
}

func TestDecideRejectsInvalidPredicate(t *testing.T) {
	for _, payload := range []string{`{}`, `{"probability":null}`, `{"probability":-0.1}`, `{"probability":1.1}`} {
		t.Run(payload, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var a map[string]any
				_ = json.Unmarshal([]byte(payload), &a)
				a["name"] = "p"
				a["type"] = "predicate"
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": []any{a}})
			}))
			defer ts.Close()
			p, err := New(Config{BaseURL: ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Decide(context.Background(), decide.Request{Model: "m", Input: "text", Questions: []decide.Question{{Name: "p", Type: decide.Predicate, Instructions: "true?"}}})
			if err == nil {
				t.Fatal("invalid probability accepted as a decision")
			}
		})
	}
}
