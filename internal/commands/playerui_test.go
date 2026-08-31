package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/music"
)

func TestFormatUpNextList(t *testing.T) {
	t.Parallel()

	items := []music.QueueItem{
		{Title: "Song A", Artist: "Artist A", DurationSec: 161},
		{Title: "Song B", Artist: "Artist B", DurationSec: 192},
	}
	got := formatUpNextList(items, 4)
	if !strings.Contains(got, "**1.**") || !strings.Contains(got, "Song A") || !strings.Contains(got, "2:41") {
		t.Fatalf("unexpected list format: %q", got)
	}
	if !strings.Contains(got, "\n\n") {
		t.Fatalf("expected spacing between items: %q", got)
	}
}

func TestQueueEmptyDescription(t *testing.T) {
	t.Parallel()

	got := queueEmptyDescription()
	if !strings.Contains(got, "Nothing queued") || !strings.Contains(got, "/play") {
		t.Fatalf("unexpected empty description: %q", got)
	}
}

func TestLoadingEmbed(t *testing.T) {
	t.Parallel()

	embed := loadingEmbed("not cute anymore")
	if embed.Author.Name != "FINDING TRACK" {
		t.Fatalf("author: got %q", embed.Author.Name)
	}
	if embed.Color != colorLoading {
		t.Fatalf("color: got %#x want %#x", embed.Color, colorLoading)
	}
	if !strings.Contains(embed.Description, "not cute anymore") {
		t.Fatalf("description should include query: %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "few seconds") {
		t.Fatalf("should mention wait time: %q", embed.Description)
	}
	if embed.Footer != nil {
		t.Fatalf("loading embed should not include footer trivia: %q", embed.Footer.Text)
	}
	if strings.ContainsAny(embed.Description, "▁▃▅▇") {
		t.Fatalf("loader should be text-only: %q", embed.Description)
	}
}

func TestPreparingEmbed(t *testing.T) {
	t.Parallel()

	embed := preparingEmbed("illit")
	if embed.Author.Name != "PREPARING AUDIO" {
		t.Fatalf("author: got %q", embed.Author.Name)
	}
	if !strings.Contains(embed.Description, "illit") {
		t.Fatalf("description: %q", embed.Description)
	}
}

func TestFormatLoadingQuery(t *testing.T) {
	t.Parallel()

	if got := formatLoadingQuery("yt:abc123"); got != "your YouTube pick" {
		t.Fatalf("yt prefix: got %q", got)
	}
	if got := formatLoadingQuery("search:hello world"); got != "hello world" {
		t.Fatalf("search prefix: got %q", got)
	}
}

func TestSongFactEmbed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	embed := songFactEmbed(ctx, music.ResolvedTrack{
		Title:       "Magnetic",
		Artist:      "ILLIT",
		DurationSec: 161,
	}, "faizur")
	if embed.Author.Name != "FUN FACT" {
		t.Fatalf("author: got %q", embed.Author.Name)
	}
	if embed.Description == "" {
		t.Fatalf("expected fact text")
	}
	if !strings.Contains(embed.Footer.Text, "ILLIT") {
		t.Fatalf("footer: %q", embed.Footer.Text)
	}
}

func TestIdleEmbed(t *testing.T) {
	t.Parallel()

	embed := idleEmbed()
	if embed.Author.Name != "PLAYER" {
		t.Fatalf("author: got %q", embed.Author.Name)
	}
	if embed.Color != colorIdle {
		t.Fatalf("color: got %#x want %#x", embed.Color, colorIdle)
	}
	if !strings.Contains(embed.Description, "Nothing playing") || !strings.Contains(embed.Description, "/play") {
		t.Fatalf("description: %q", embed.Description)
	}
}

func TestNowPlayingDescription(t *testing.T) {
	t.Parallel()

	got := nowPlayingDescription("ILLIT", 147)
	if !strings.Contains(got, "ILLIT") || !strings.Contains(got, "2:27") {
		t.Fatalf("description: %q", got)
	}
	if strings.Contains(got, "`") {
		t.Fatalf("should not include timer formatting: %q", got)
	}
}

func TestPlayErrorEmbed(t *testing.T) {
	t.Parallel()

	embed := playErrorEmbed(music.DescribePlaybackError(errors.New("test")))
	if embed.Author.Name != "COULD NOT PLAY" {
		t.Fatalf("author: %q", embed.Author.Name)
	}
	if embed.Color != colorError {
		t.Fatalf("color: %#x", embed.Color)
	}
	if embed.Title == "" || embed.Description == "" {
		t.Fatalf("missing title or description")
	}
}

func TestFormatAutocompleteChoice(t *testing.T) {
	t.Parallel()

	got := formatAutocompleteChoice("ILLIT - I'm Not Cute Anymore (Official MV)")
	if !strings.Contains(got, "—") {
		t.Fatalf("unexpected format: %q", got)
	}
	if strings.Contains(got, "🔍") {
		t.Fatalf("should not prefix emoji: %q", got)
	}
}

func TestButtonsHaveNoEmoji(t *testing.T) {
	t.Parallel()

	buttons := []discordgo.Button{
		transportButton(false, "guild"),
		transportButton(true, "guild"),
		skipButton("guild"),
		queueButton("guild"),
		stopButton("guild"),
		funFactButton("guild"),
	}
	for _, btn := range buttons {
		if btn.Emoji != nil {
			t.Fatalf("button %q should not have emoji", btn.Label)
		}
	}
}
