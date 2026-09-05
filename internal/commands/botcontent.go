package commands

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/music"
)

var loadingLines = []string{
	"Asking YouTube nicely…",
	"Tunneling through WARP…",
	"FR is on the aux.",
	"Spinning up the stream…",
	"Convincing the algorithm this is fine…",
	"Fetching bytes from the cloud…",
	"Warming up ffmpeg…",
	"One sec — good music takes a moment.",
	"Routing around datacenter blocks…",
	"Loading vibes…",
}

var preparingLines = []string{
	"Almost there — piping audio into voice…",
	"Stream locked in. Getting loud…",
	"Final checks before playback…",
	"Handing off to Linkdave…",
	"Buffering the good part…",
}

func randomLoadingLine() string {
	return loadingLines[rand.IntN(len(loadingLines))]
}

func randomPreparingLine() string {
	return preparingLines[rand.IntN(len(preparingLines))]
}

func formatLoadingQuery(query string) string {
	query = strings.TrimSpace(query)
	switch {
	case query == "":
		return "your request"
	case strings.HasPrefix(query, "yt:"):
		return "your YouTube pick"
	case strings.HasPrefix(query, "search:"):
		return truncateRunes(strings.TrimPrefix(query, "search:"), 120)
	default:
		return truncateRunes(query, 120)
	}
}

func loadingDescription(query string) string {
	label := formatLoadingQuery(query)
	return fmt.Sprintf("**%s**\n\n%s\n\n*Hang tight — this usually takes a few seconds.*", label, randomLoadingLine())
}

func preparingDescription(query string) string {
	label := formatLoadingQuery(query)
	return fmt.Sprintf("**%s**\n\n%s", label, randomPreparingLine())
}

func songFactEmbed(ctx context.Context, track music.ResolvedTrack, requester string) *discordgo.MessageEmbed {
	fact := music.SongFact(ctx, track.Artist, track.Title, track.DurationSec, requester)
	embed := &discordgo.MessageEmbed{
		Color:       colorFunFact,
		Author:      &discordgo.MessageEmbedAuthor{Name: "FUN FACT"},
		Description: fact,
	}
	return withSiteFooter(embed, songFactSubtitle(track))
}

func songFactSubtitle(track music.ResolvedTrack) string {
	title := displaySongTitle(track)
	if track.Artist != "" && title != "" {
		return track.Artist + " · " + title
	}
	return title
}
