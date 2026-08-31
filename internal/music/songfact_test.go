package music

import (
	"context"
	"strings"
	"testing"
)

func TestTrimSongFact(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", maxSongFactLen+20)
	got := trimSongFact(long)
	if len([]rune(got)) != maxSongFactLen {
		t.Fatalf("length: got %d want %d", len([]rune(got)), maxSongFactLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix: %q", got)
	}
}

func TestFallbackSongFact(t *testing.T) {
	t.Parallel()

	got := fallbackSongFact("ILLIT", "Magnetic", 161, "faizur")
	if got == "" {
		t.Fatal("expected fallback text")
	}
	if !strings.Contains(got, "ILLIT") && !strings.Contains(got, "Magnetic") && !strings.Contains(got, "faizur") {
		t.Fatalf("unexpected fallback: %q", got)
	}
}

func TestSongFactQueries(t *testing.T) {
	t.Parallel()

	queries := songFactQueries("ILLIT", "Magnetic")
	if len(queries) < 2 {
		t.Fatalf("expected multiple queries: %v", queries)
	}
}

func TestSongFactUsesFallbackWithoutNetwork(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := SongFact(ctx, "Test Artist", "Test Song", 120, "dj")
	if got == "" {
		t.Fatal("expected fallback when lookup is unavailable")
	}
}
