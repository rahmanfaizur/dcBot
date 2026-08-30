package music

import "testing"

func TestSanitizeYTDLPOutput(t *testing.T) {
	output := "Deprecated Feature: Support for Python version 3.10 has been deprecated.\nRick Astley - Never Gonna Give You Up\nhttps://i.ytimg.com/vi/abc/hqdefault.jpg\nhttps://www.youtube.com/watch?v=abc\n213"
	title, thumb, page, dur := parseYTDLPPrint(output)
	if title != "Rick Astley - Never Gonna Give You Up" {
		t.Fatalf("unexpected title: %q", title)
	}
	if thumb == "" || page == "" {
		t.Fatalf("expected thumbnail and page url, got thumb=%q page=%q", thumb, page)
	}
	if dur != 213 {
		t.Fatalf("expected duration 213, got %d", dur)
	}
}

func TestSplitTrackTitle(t *testing.T) {
	artist, title := splitTrackTitle("ILLIT (아일릿) - NOT CUTE ANYMORE Official MV")
	if artist != "ILLIT (아일릿)" || title != "NOT CUTE ANYMORE" {
		t.Fatalf("got artist=%q title=%q", artist, title)
	}

	artist, title = splitTrackTitle("RedOne ft. Enrique Iglesias | Don't You Need Somebody")
	if artist != "RedOne ft. Enrique Iglesias" || title != "Don't You Need Somebody" {
		t.Fatalf("got artist=%q title=%q", artist, title)
	}
}
