package music

import "testing"

func TestIsPlaylistURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"https://www.youtube.com/playlist?list=PLabc123", true},
		{"https://www.youtube.com/watch?v=abc&list=PLabc123", true},
		{"https://open.spotify.com/playlist/abc", true},
		{"https://open.spotify.com/album/abc", true},
		{"https://www.youtube.com/watch?v=abc", false},
		{"illit magnetic", false},
	}
	for _, tc := range tests {
		if got := IsPlaylistURL(tc.input); got != tc.want {
			t.Fatalf("IsPlaylistURL(%q) = %v want %v", tc.input, got, tc.want)
		}
	}
}
