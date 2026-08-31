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

func TestSearchTextFor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain query", "not cute anymore", "not cute anymore"},
		{"search prefix", "search:blinding lights", "blinding lights"},
		{"video id has no words", "yt:dQw4w9WgXcQ", ""},
		{"link has no words", "https://www.youtube.com/watch?v=abc", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := searchTextFor(tt.input); got != tt.want {
				t.Fatalf("searchTextFor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFallbackTargetFor(t *testing.T) {
	tests := []struct {
		name   string
		target string
		input  string
		title  string
		want   string
	}{
		{
			name:   "search falls back to the user's own words",
			target: "ytsearch1:not cute anymore",
			input:  "not cute anymore",
			title:  "ILLIT (아일릿) 'NOT CUTE ANYMORE' Official MV",
			want:   "scsearch1:not cute anymore",
		},
		{
			name:   "link falls back to the resolved title",
			target: "https://www.youtube.com/watch?v=abc",
			input:  "https://www.youtube.com/watch?v=abc",
			title:  "Rick Astley - Never Gonna Give You Up",
			want:   "scsearch1:Rick Astley - Never Gonna Give You Up",
		},
		{
			name:   "soundcloud needs no fallback",
			target: "https://soundcloud.com/artist/track",
			input:  "https://soundcloud.com/artist/track",
			title:  "Some Track",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackTargetFor(tt.target, tt.input, tt.title); got != tt.want {
				t.Fatalf("fallbackTargetFor(%q, %q, %q) = %q, want %q", tt.target, tt.input, tt.title, got, tt.want)
			}
		})
	}
}
