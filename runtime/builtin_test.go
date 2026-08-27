package runtime

import (
	"testing"
	"time"
)

func TestProviderConfigHTTPClientTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout int
		want    time.Duration
	}{
		{
			name: "unset has no whole-request timeout",
			want: 0,
		},
		{
			name:    "explicit timeout is measured in milliseconds",
			timeout: 1_500,
			want:    1500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := (ProviderConfig{Timeout: tt.timeout}).httpClient()
			if client.Timeout != tt.want {
				t.Fatalf("http client timeout = %s, want %s", client.Timeout, tt.want)
			}
		})
	}
}
