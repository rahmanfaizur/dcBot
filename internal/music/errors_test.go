package music

import (
	"errors"
	"strings"
	"testing"
)

func TestPlaybackErrorInfoDefault(t *testing.T) {
	t.Parallel()

	info := DescribePlaybackError(errors.New("some unknown failure"))
	if info.Title != "Track unavailable" {
		t.Fatalf("title: %q", info.Title)
	}
	if !strings.Contains(info.Description, "region-locked") {
		t.Fatalf("description: %q", info.Description)
	}
}

func TestPlaybackErrorInfoVoice(t *testing.T) {
	t.Parallel()

	info := DescribePlaybackError(errors.New("timed out waiting for linkdave"))
	if info.Title != "Voice connection failed" {
		t.Fatalf("title: %q", info.Title)
	}
}

func TestPlaybackErrorInfoYouTubeBotBlock(t *testing.T) {
	t.Parallel()

	info := DescribePlaybackError(errors.New(`ERROR: Sign in to confirm you're not a bot`))
	if info.Title != "YouTube blocked this server" {
		t.Fatalf("title: %q", info.Title)
	}
}

func TestFriendlyControlErrorSanitizesLinkdave(t *testing.T) {
	t.Parallel()

	err := errors.New(`pausing playback: linkdave POST /sessions/15560329-dc25-4ea4-b010-17004b728738/players/1445334259245121599/pause: {"error":"player not found"}`)
	got := FriendlyControlError(err)
	if strings.Contains(got, "linkdave") || strings.Contains(got, "POST") || strings.Contains(got, "{") {
		t.Fatalf("leaked technical error: %q", got)
	}
	if !strings.Contains(got, "/play") {
		t.Fatalf("expected actionable message: %q", got)
	}
}
