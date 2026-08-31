package music

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestPrepareCookiesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "readonly.txt")
	if err := os.WriteFile(src, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := PrepareCookiesFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("expected writable cookies path")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("writable cookies missing: %v", err)
	}
}

func TestIsYouTubeTarget(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"ytsearch1:hello", true},
		{"https://www.youtube.com/watch?v=abc", true},
		{"https://youtu.be/abc", true},
		{"scsearch1:hello", false},
		{"https://soundcloud.com/artist/track", false},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := isYouTubeTarget(tt.target); got != tt.want {
				t.Fatalf("isYouTubeTarget(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

// Non-YouTube targets must not be retried across YouTube player clients.
func TestStreamArgVariantsSkipsYouTubeClientSweep(t *testing.T) {
	y := YTDLP{}

	soundCloud := y.streamArgVariants("scsearch1:hello")
	if len(soundCloud) != len(youtubeStreamFormats) {
		t.Fatalf("got %d SoundCloud variants, want %d", len(soundCloud), len(youtubeStreamFormats))
	}
	for _, args := range soundCloud {
		for _, arg := range args {
			if strings.Contains(arg, "player_client") {
				t.Fatalf("SoundCloud variant should not set player_client: %v", args)
			}
		}
	}

	want := len(youtubeStreamClients) * len(youtubeStreamFormats)
	if got := len(y.streamArgVariants("ytsearch1:hello")); got != want {
		t.Fatalf("got %d YouTube variants, want %d", got, want)
	}
}

// Each variant must end with the target so yt-dlp writes media to stdout.
func TestStreamArgVariantsOutputFlags(t *testing.T) {
	variants := YTDLP{}.streamArgVariants("ytsearch1:hello")
	if len(variants) == 0 {
		t.Fatal("expected at least one variant")
	}

	for _, args := range variants {
		if got := args[len(args)-1]; got != "ytsearch1:hello" {
			t.Fatalf("variant should end with the target, got %q in %v", got, args)
		}
		tail := strings.Join(args[len(args)-3:], " ")
		if tail != "-o - ytsearch1:hello" {
			t.Fatalf("variant should write to stdout, got %q", tail)
		}
	}
}

func TestParseFlatDuration(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"213", 213},
		{"213.6", 214},
		{"NA", 0},
		{"", 0},
		{"-5", 0},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := parseFlatDuration(tt.value); got != tt.want {
				t.Fatalf("parseFlatDuration(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestStreamArgVariantsIncludeProxy(t *testing.T) {
	variants := YTDLP{Proxy: "socks5://127.0.0.1:1080"}.streamArgVariants("ytsearch1:hello")
	if len(variants) == 0 {
		t.Fatal("expected variants")
	}
	found := false
	for i, arg := range variants[0] {
		if arg == "--proxy" && i+1 < len(variants[0]) && variants[0][i+1] == "socks5://127.0.0.1:1080" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --proxy socks5://127.0.0.1:1080 in %v", variants[0])
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
