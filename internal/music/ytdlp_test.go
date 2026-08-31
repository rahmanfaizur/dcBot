package music

import "testing"

func TestParseYTDLPMetadataJSON_SearchResult(t *testing.T) {
	const sample = `{
		"entries": [{
			"fulltitle": "Rick Astley - Never Gonna Give You Up (Official Video) (4K Remaster)",
			"thumbnail": "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg",
			"original_url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"duration": 213,
			"display_id": "dQw4w9WgXcQ"
		}],
		"webpage_url": "ytsearch1:never gonna give you up",
		"extractor": "youtube:search"
	}`

	title, thumb, page, dur, err := parseYTDLPMetadataJSON(sample)
	if err != nil {
		t.Fatalf("parseYTDLPMetadataJSON: %v", err)
	}
	if title != "Rick Astley - Never Gonna Give You Up (Official Video)" {
		t.Fatalf("unexpected title: %q", title)
	}
	if thumb == "" {
		t.Fatal("expected thumbnail")
	}
	if page != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("unexpected page url: %q", page)
	}
	if dur != 213 {
		t.Fatalf("expected duration 213, got %d", dur)
	}
}

func TestStreamTargetForPlayback(t *testing.T) {
	got := streamTargetForPlayback(
		"ytsearch1:hello",
		"https://www.youtube.com/watch?v=abc123",
	)
	if got != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("got %q", got)
	}
}
