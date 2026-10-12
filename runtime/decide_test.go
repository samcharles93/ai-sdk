package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/decide"
)

func TestDecideGating(t *testing.T) {
	RegisterBuiltinClasses()
	for _, tt := range []struct {
		name, class, modality string
		caps                  []Capability
		allowed               bool
		listing               bool
	}{
		{"server decision", "openai-compatible", "decisions", nil, true, true},
		{"server text", "openai-compatible", "text", nil, false, true},
		{"unknown modality", "openai-compatible", "", nil, true, true},
		{"explicit decision", "openai-compatible", "text", []Capability{CapabilityDecide}, true, false},
		{"explicit text", "openai-compatible", "decisions", []Capability{CapabilityChat}, false, false},
		{"openai decision", "openai", "", []Capability{CapabilityDecide}, true, false},
		{"openai excluded", "openai", "", []Capability{CapabilityChat}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			posts, gets := 0, 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					gets++
					modalities := "[]"
					if tt.modality != "" {
						modalities = fmt.Sprintf("[%q]", tt.modality)
					}
					_, _ = fmt.Fprintf(w, `{"data":[{"id":"m","architecture":{"output_modalities":%s}}]}`, modalities)
					return
				}
				posts++
				if r.Method != "POST" {
					t.Error(r.Method)
				}
				if tt.class == "openai" {
					if r.URL.Path != "/v1/decisions" {
						t.Error(r.URL)
					}
					_, _ = w.Write([]byte(`{"model":"m","answers":[{"type":"predicate","name":"q","probability":0.9}]}`))
				} else {
					if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "" {
						t.Error(r.URL)
					}
					_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"noul","noul":0.9}}}`))
				}
			}))
			defer ts.Close()
			rt := NewRuntime(Config{Providers: map[string]ProviderConfig{"p": {ID: "p", Class: tt.class, BaseURL: ts.URL, Auth: AuthConfig{Type: AuthTypeNone}, Models: []ModelConfig{{ID: "m", Capabilities: tt.caps}}}}})
			out, err := rt.Decide(context.Background(), "p/m", decide.Request{Model: "wrong", Input: "evidence", Questions: []decide.Question{{Name: "q", Type: decide.Predicate, Instructions: "true?"}}})
			if !tt.allowed {
				if !errors.Is(err, ErrCapabilityNotSupported) || posts != 0 {
					t.Fatalf("posts %d err %v", posts, err)
				}
			} else if err != nil || posts != 1 || out.Answers[0].Predicate.Probability != 0.9 {
				t.Fatalf("posts %d out %+v err %v", posts, out, err)
			}
			if (gets == 1) != tt.listing {
				t.Fatalf("GETs %d", gets)
			}
		})
	}
}
