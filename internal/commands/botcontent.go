package commands

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/bwmarrin/discordgo"
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

var botFacts = []string{
	"I run 24/7 on an Azure VM in Korea.",
	"YouTube traffic goes through Cloudflare WARP so datacenter IPs don't get blocked.",
	"Spotify links are resolved via YouTube under the hood.",
	"Built with Go, yt-dlp, ffmpeg, and Linkdave.",
	"Only yt-dlp uses the WARP proxy — SSH and Discord stay on normal networking.",
	"Autocomplete shows real YouTube search results with durations.",
	"I stream audio through a pipe so proxy IPs don't break playback.",
	"You can paste YouTube, Spotify, or SoundCloud links — or just search.",
}

func randomLoadingLine() string {
	return loadingLines[rand.IntN(len(loadingLines))]
}

func randomPreparingLine() string {
	return preparingLines[rand.IntN(len(preparingLines))]
}

func randomBotFact() string {
	return botFacts[rand.IntN(len(botFacts))]
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

func funFactEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       colorQueueList,
		Author:      &discordgo.MessageEmbedAuthor{Name: "FUN FACT"},
		Description: randomBotFact(),
	}
}
