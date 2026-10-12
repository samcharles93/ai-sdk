package systemone

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/decide"
)

func TestDecideWire(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		images bool
	}{{"text", 200, false}, {"images", 200, true}, {"unsupported", 501, false}} {
		t.Run(tt.name, func(t *testing.T) {
			req := decide.Request{Model: "clef", Input: "evidence", Questions: []decide.Question{
				{Name: "z", Type: decide.Predicate, Instructions: "is it true"},
				{Name: "a", Type: decide.Choice, Instructions: "choose", Choices: []decide.Option{{Value: decide.Bool(true), Description: "yes"}, {Value: decide.String("true")}}},
				{Name: "s", Type: decide.Score, Instructions: "rate", Levels: []decide.Level{{Label: "low", Description: "can wait"}, {Label: "high"}}},
			}}
			if tt.images {
				req.Input = ""
				req.Messages = []decide.Message{{Text: "evidence", Images: []string{"data:image/png;base64,AA=="}}}
			}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/systemone" || r.Method != "POST" || r.Header.Get("Authorization") != "" {
					t.Errorf("bad request %s", r.URL)
				}
				var body struct {
					Model     string
					State     json.RawMessage
					Images    []string
					Questions map[string]question
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Model != "clef" || len(body.Questions) != 3 || body.Questions["z"].Type != "noul" {
					t.Errorf("body %+v", body)
				}
				c, ok := body.Questions["a"].Criteria.(map[string]any)
				if !ok || len(c) != 2 || c["true"] != "yes" {
					t.Errorf("criteria %+v", c)
				}
				if _, ok := c[`"true"`]; !ok {
					t.Error("string choice lost")
				}
				levels, ok := body.Questions["s"].Criteria.([]any)
				if !ok || len(levels) != 2 || levels[0] != "low: can wait" || levels[1] != "high" {
					t.Error(levels)
				}
				if tt.images {
					if len(body.Images) != 1 || body.Images[0] != "data:image/png;base64,AA==" || string(body.State) != `[{"content":"evidence","role":"user"}]` {
						t.Errorf("images %+v", body)
					}
				} else if string(body.State) != `"evidence"` {
					t.Error(string(body.State))
				}
				w.WriteHeader(tt.status)
				if tt.status != 200 {
					return
				}
				_, _ = w.Write([]byte(`{"model":"clef","answers":{"s":{"type":"score","score":0.8,"legend":{"0":"low: can wait","1":"high"},"probabilities":{"0":0.2,"1":0.8},"confidence":0.6},"a":{"type":"choice","choice":"true","probabilities":{"true":0.9,"\"true\"":0.1},"confidence":0.8},"z":{"type":"noul","noul":0.9}},"usage":{"input_tokens":42,"output_tokens":0}}`))
			}))
			defer ts.Close()
			p, err := New(Config{BaseURL: ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.Decide(context.Background(), req)
			if tt.status == 501 {
				if !errors.Is(err, decide.ErrUnsupported) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Answers) != 3 || out.Answers[0].Name != "z" || out.Answers[1].Name != "a" || out.Answers[2].Name != "s" || out.Answers[0].Predicate.Probability != 0.9 || out.Answers[2].Score.Score != 0.8 || out.Answers[2].Confidence != 0.6 || out.Answers[2].Score.Probabilities[0].Label != "low" || out.Usage.TotalTokens != 42 {
				t.Fatalf("%+v", out)
			}
			v, ok := out.Answers[1].Choice.Choice.BoolValue()
			if !ok || !v {
				t.Fatal(out.Answers[1])
			}
			s, ok := out.Answers[1].Choice.Probabilities[1].Value.StringValue()
			if !ok || s != "true" {
				t.Fatal(out.Answers[1])
			}
		})
	}
}

func TestModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" {
			t.Error(r.URL)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"clef","architecture":{"output_modalities":["decisions"]}},{"id":"chat","architecture":{"output_modalities":["text"]}}]}`))
	}))
	defer ts.Close()
	p, err := New(Config{BaseURL: ts.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Architecture.OutputModalities[0] != "decisions" || models[1].Architecture.OutputModalities[0] != "text" {
		t.Fatal(models)
	}
}
