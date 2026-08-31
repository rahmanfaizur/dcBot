package music

import "testing"

func TestNormalizeResolveTarget(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"search:hello", "ytsearch1:hello"},
		{"yt:abc123", "https://www.youtube.com/watch?v=abc123"},
		{"plain query", "ytsearch1:plain query"},
		{"https://www.youtube.com/watch?v=abc", "https://www.youtube.com/watch?v=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeResolveTarget(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeResolveTarget(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
