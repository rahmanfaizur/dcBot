package music

import (
	"strings"
	"testing"
)

func TestQueueEnqueueAndAdvance(t *testing.T) {
	tests := []struct {
		name      string
		items     []QueueItem
		wantNext  string
		wantLeft  int
		wantEmpty bool
	}{
		{
			name: "single item advances to empty",
			items: []QueueItem{
				{Title: "one", StreamURL: "https://example.com/1.mp3"},
			},
			wantNext:  "one",
			wantLeft:  0,
			wantEmpty: true,
		},
		{
			name: "multiple items advance in order",
			items: []QueueItem{
				{Title: "one", StreamURL: "https://example.com/1.mp3"},
				{Title: "two", StreamURL: "https://example.com/2.mp3"},
			},
			wantNext:  "one",
			wantLeft:  1,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQueue()
			for _, item := range tt.items {
				q.Enqueue(item)
			}
			q.SetActive(true)

			next, ok := q.Advance()
			if !ok {
				t.Fatalf("expected next item")
			}
			if next.Title != tt.wantNext {
				t.Fatalf("got title %q, want %q", next.Title, tt.wantNext)
			}
			if q.Len() != tt.wantLeft {
				t.Fatalf("got len %d, want %d", q.Len(), tt.wantLeft)
			}

			_, ok = q.Advance()
			if ok == tt.wantEmpty {
				t.Fatalf("advance empty mismatch: got ok=%v wantEmpty=%v", ok, tt.wantEmpty)
			}
		})
	}
}

func TestQueueSkip(t *testing.T) {
	q := NewQueue()
	q.SetNowPlaying(QueueItem{Title: "now", StreamURL: "https://example.com/now.mp3"})
	q.Enqueue(QueueItem{Title: "next", StreamURL: "https://example.com/next.mp3"})

	item, ok := q.Skip()
	if !ok || item.Title != "next" {
		t.Fatalf("skip did not return next track: %#v ok=%v", item, ok)
	}
}

func TestClassifyInput(t *testing.T) {
	tests := []struct {
		input string
		want  inputKind
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", inputYouTube},
		{"https://youtu.be/dQw4w9WgXcQ", inputYouTube},
		{"https://open.spotify.com/track/abc", inputSpotify},
		{"https://soundcloud.com/artist/track", inputSoundCloud},
		{"https://cdn.example.com/song.mp3", inputDirect},
		{"never gonna give you up", inputSearch},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := classifyInput(strings.TrimSpace(tt.input)); got != tt.want {
				t.Fatalf("classifyInput(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
