package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/music"
)

const (
	colorNowPlaying = 0xE91E63
	colorQueued     = 0x43B581
	colorQueueList  = 0x5865F2
	colorLoading    = 0xFF9F0A
	colorPaused     = 0xF9A825
	colorIdle       = 0x5C5F66
	colorError      = 0xFF5C8A
	colorVoice      = 0x5865F2

	queueListPreviewLimit = 4
)

func loadingEmbed(query string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       colorLoading,
		Author:      &discordgo.MessageEmbedAuthor{Name: "FINDING TRACK"},
		Description: loadingDescription(query),
	}
}

func preparingEmbed(query string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       colorLoading,
		Author:      &discordgo.MessageEmbedAuthor{Name: "PREPARING AUDIO"},
		Description: preparingDescription(query),
	}
}

func idleEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       colorIdle,
		Author:      &discordgo.MessageEmbedAuthor{Name: "PLAYER"},
		Description: "**Nothing playing.**\nUse `/play` to start listening.",
	}
}

func nowPlayingEmbed(track music.ResolvedTrack, requester string, state music.PlaybackState) *discordgo.MessageEmbed {
	author := "NOW PLAYING"
	color := colorNowPlaying
	if state.Paused {
		author = "PAUSED"
		color = colorPaused
	}

	duration := track.DurationSec
	if duration == 0 && state.Now != nil {
		duration = state.Now.DurationSec
	}

	embed := &discordgo.MessageEmbed{
		Color: color,
		Author: &discordgo.MessageEmbedAuthor{
			Name: author,
		},
		Title:       displaySongTitle(track),
		Description: nowPlayingDescription(track.Artist, duration),
	}

	if track.Thumbnail != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: track.Thumbnail}
	}

	var footer []string
	if requester != "" {
		footer = append(footer, "Requested by "+requester)
	}
	if state.Upcoming > 0 {
		footer = append(footer, fmt.Sprintf("%d in queue", state.Upcoming))
	}
	if len(footer) > 0 {
		embed.Footer = &discordgo.MessageEmbedFooter{Text: strings.Join(footer, " · ")}
	}
	return embed
}

func nowPlayingDescription(artist string, durationSec int) string {
	var parts []string
	if artist != "" {
		parts = append(parts, artist)
	}
	if durationSec > 0 {
		parts = append(parts, music.FormatDuration(durationSec))
	}
	return strings.Join(parts, "\n")
}

func playerControlsForGuild(guildID string, state music.PlaybackState) []discordgo.MessageComponent {
	rows := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			transportButton(state.Paused, guildID),
			skipButton(guildID),
			queueButton(guildID),
			stopButton(guildID),
		}},
	}

	track := music.ResolvedTrack{}
	if state.Now != nil {
		track = queueItemToTrack(*state.Now)
	}
	linkRow := linkButtonsForGuild(guildID, track)
	if len(linkRow) > 0 {
		rows = append(rows, discordgo.ActionsRow{Components: linkRow})
	}
	return rows
}

func linkButtonsForGuild(guildID string, track music.ResolvedTrack) []discordgo.MessageComponent {
	var buttons []discordgo.MessageComponent

	youtubeURL := ""
	if isYouTubeURL(track.PageURL) {
		youtubeURL = track.PageURL
	} else if track.Title != "" || track.Artist != "" {
		youtubeURL = youtubeSearchURL(track.Artist, track.Title)
	}
	if youtubeURL != "" {
		buttons = append(buttons, discordgo.Button{
			Label: "YouTube",
			Style: discordgo.LinkButton,
			URL:   youtubeURL,
		})
	}

	spotifyURL := ""
	if isSpotifyURL(track.PageURL) {
		spotifyURL = track.PageURL
	} else if track.Title != "" || track.Artist != "" {
		spotifyURL = spotifySearchURL(track.Artist, track.Title)
	}
	if spotifyURL != "" {
		buttons = append(buttons, discordgo.Button{
			Label: "Spotify",
			Style: discordgo.LinkButton,
			URL:   spotifyURL,
		})
	}

	buttons = append(buttons, funFactButton(guildID))
	return buttons
}

func funFactButton(guildID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Fun Fact",
		Style:    discordgo.SecondaryButton,
		CustomID: "music:fact:" + guildID,
	}
}

func youtubeSearchURL(artist, title string) string {
	q := strings.TrimSpace(strings.Join([]string{artist, title}, " "))
	return "https://www.youtube.com/results?search_query=" + url.QueryEscape(q)
}

func spotifySearchURL(artist, title string) string {
	q := strings.TrimSpace(strings.Join([]string{artist, title}, " "))
	return "https://open.spotify.com/search/" + url.PathEscape(q)
}

func transportButton(paused bool, guildID string) discordgo.Button {
	if paused {
		return discordgo.Button{
			Label:    "Resume",
			Style:    discordgo.PrimaryButton,
			CustomID: "music:resume:" + guildID,
		}
	}
	return discordgo.Button{
		Label:    "Pause",
		Style:    discordgo.PrimaryButton,
		CustomID: "music:pause:" + guildID,
	}
}

func skipButton(guildID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Skip",
		Style:    discordgo.SecondaryButton,
		CustomID: "music:skip:" + guildID,
	}
}

func queueButton(guildID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Queue",
		Style:    discordgo.SecondaryButton,
		CustomID: "music:queue:" + guildID,
	}
}

func stopButton(guildID string) discordgo.Button {
	return discordgo.Button{
		Label:    "Stop",
		Style:    discordgo.DangerButton,
		CustomID: "music:stop:" + guildID,
	}
}

func isYouTubeURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "youtube.com/") || strings.Contains(lower, "youtu.be/")
}

func isSpotifyURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "spotify.com/")
}

func queuedEmbed(track music.ResolvedTrack, position int, requester string) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Color: colorQueued,
		Author: &discordgo.MessageEmbedAuthor{
			Name: "ADDED TO QUEUE",
		},
		Title: displaySongTitle(track),
	}
	if track.Artist != "" {
		embed.Description = track.Artist
	}
	if position > 0 {
		embed.Fields = []*discordgo.MessageEmbedField{
			{Name: "POSITION", Value: fmt.Sprintf("#%d", position), Inline: true},
		}
		if track.DurationSec > 0 {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name: "DURATION", Value: music.FormatDuration(track.DurationSec), Inline: true,
			})
		}
	} else if track.DurationSec > 0 {
		embed.Fields = []*discordgo.MessageEmbedField{
			{Name: "DURATION", Value: music.FormatDuration(track.DurationSec), Inline: true},
		}
	}
	if track.Thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: track.Thumbnail}
	}
	if requester != "" {
		embed.Footer = &discordgo.MessageEmbedFooter{Text: "Requested by " + requester}
	}
	return embed
}

func queueListEmbed(snapshot music.QueueSnapshot) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Color: colorQueueList,
		Author: &discordgo.MessageEmbedAuthor{
			Name: "QUEUE",
		},
	}

	if snapshot.Now == nil && len(snapshot.Upcoming) == 0 {
		embed.Description = queueEmptyDescription()
		return embed
	}

	if snapshot.Now != nil {
		if snapshot.Now.Thumbnail != "" {
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: snapshot.Now.Thumbnail}
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "NOW PLAYING",
			Value:  queueTrackValue(snapshot.Now.Title, snapshot.Now.Artist, snapshot.Now.DurationSec),
			Inline: false,
		})
	}

	if len(snapshot.Upcoming) > 0 {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("UP NEXT · %d", len(snapshot.Upcoming)),
			Value:  formatUpNextList(snapshot.Upcoming, queueListPreviewLimit),
			Inline: false,
		})
	}

	remaining := len(snapshot.Upcoming) - queueListPreviewLimit
	if remaining > 0 {
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("+%d more", remaining),
		}
	}
	return embed
}

func queueTrackValue(title, artist string, durationSec int) string {
	title = cleanDisplayTitle(title)
	if title == "" {
		title = "Unknown track"
	}

	var lines []string
	lines = append(lines, "**"+title+"**")

	var meta []string
	if artist != "" {
		meta = append(meta, artist)
	}
	if durationSec > 0 {
		meta = append(meta, music.FormatDuration(durationSec))
	}
	if len(meta) > 0 {
		lines = append(lines, strings.Join(meta, " · "))
	}
	return strings.Join(lines, "\n")
}

func formatUpNextList(items []music.QueueItem, limit int) string {
	if len(items) == 0 {
		return ""
	}
	if limit > len(items) {
		limit = len(items)
	}

	var blocks []string
	for i := 0; i < limit; i++ {
		item := items[i]
		title := cleanDisplayTitle(item.Title)
		if title == "" {
			title = "Unknown track"
		}

		var lines []string
		lines = append(lines, fmt.Sprintf("**%d.** %s", i+1, title))

		var meta []string
		if item.Artist != "" {
			meta = append(meta, item.Artist)
		}
		if item.DurationSec > 0 {
			meta = append(meta, music.FormatDuration(item.DurationSec))
		}
		if len(meta) > 0 {
			lines = append(lines, strings.Join(meta, " · "))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func queueEmptyDescription() string {
	return "Nothing queued.\nUse `/play` to start listening."
}

func joinSuccessEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       colorVoice,
		Author:      &discordgo.MessageEmbedAuthor{Name: "VOICE"},
		Description: "Joined your voice channel.",
	}
}

func leaveSuccessEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       colorVoice,
		Author:      &discordgo.MessageEmbedAuthor{Name: "VOICE"},
		Description: "Left the voice channel.",
	}
}

func statusEmbed(label, detail string, color int) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       color,
		Author:      &discordgo.MessageEmbedAuthor{Name: strings.ToUpper(label)},
		Description: detail,
	}
}

func playErrorEmbed(info music.PlaybackError) *discordgo.MessageEmbed {
	title := info.Title
	if title == "" {
		title = "Track unavailable"
	}
	description := info.Description
	if description == "" {
		description = "I couldn't load that one — it may be region-locked or removed. Try another link or search again with `/play`."
	}
	return &discordgo.MessageEmbed{
		Color:       colorError,
		Author:      &discordgo.MessageEmbedAuthor{Name: "COULD NOT PLAY"},
		Title:       title,
		Description: description,
	}
}

func voiceErrorEmbed(detail string) *discordgo.MessageEmbed {
	if detail == "" {
		detail = "I couldn't join your voice channel — make sure I'm allowed to connect and speak."
	}
	return &discordgo.MessageEmbed{
		Color:       colorError,
		Author:      &discordgo.MessageEmbedAuthor{Name: "COULD NOT JOIN"},
		Title:       "Voice unavailable",
		Description: detail,
	}
}

func displaySongTitle(track music.ResolvedTrack) string {
	if track.Title != "" {
		return track.Title
	}
	return cleanDisplayTitle(track.Artist)
}

func cleanDisplayTitle(s string) string {
	return music.CleanDisplayTitle(s)
}

func requesterName(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.Username
	}
	if i.User != nil {
		return i.User.Username
	}
	return ""
}
