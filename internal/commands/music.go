package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/music"
)

// MusicCommands returns slash commands for voice playback.
func MusicCommands(svc *music.Service, panel *PlayerPanel) []Command {
	return []Command{
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "join",
				Description: "Join your current voice channel.",
			},
			Handler: joinHandler(svc),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "leave",
				Description: "Leave the voice channel.",
			},
			Handler: leaveHandler(svc),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "play",
				Description: "Play a song from a link or search query.",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:         discordgo.ApplicationCommandOptionString,
						Name:         "query",
						Description:  "Search, YouTube/Spotify playlist, or paste a link",
						Required:     true,
						Autocomplete: true,
					},
				},
			},
			Handler:      playHandler(svc, panel),
			Autocomplete: playAutocomplete(svc),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "skip",
				Description: "Skip the current track.",
			},
			Handler: skipHandler(svc),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "queue",
				Description: "Show the current music queue.",
			},
			Handler: queueHandler(svc),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "nowplaying",
				Description: "Show the track that is currently playing with controls.",
			},
			Handler: nowPlayingHandler(svc, panel),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "pause",
				Description: "Pause playback.",
			},
			Handler: pauseHandler(svc),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "resume",
				Description: "Resume playback.",
			},
			Handler: resumeHandler(svc),
		},
	}
}

func joinHandler(svc *music.Service) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		channelID := music.MemberVoiceChannel(s, i)
		if channelID == "" {
			return respondEphemeral(s, i, "Join a voice channel first.")
		}

		if err := deferChannel(s, i); err != nil {
			return err
		}

		joinCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()

		if err := svc.Join(joinCtx, s, botUserID(s), i.GuildID, channelID); err != nil {
			_ = editDeferredEmbed(s, i, voiceErrorEmbed(music.FriendlyControlError(err)))
			return nil
		}

		return editDeferredEmbed(s, i, joinSuccessEmbed())
	}
}

func leaveHandler(svc *music.Service) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		if err := svc.Leave(ctx, s, botUserID(s), i.GuildID); err != nil {
			return respondChannelEmbed(s, i, voiceErrorEmbed(music.FriendlyControlError(err)))
		}
		return respondChannelEmbed(s, i, leaveSuccessEmbed())
	}
}

func playHandler(svc *music.Service, panel *PlayerPanel) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		query := optionString(i, "query")
		if query == "" {
			return respondEphemeral(s, i, "Provide a query or link.")
		}

		channelID := music.MemberVoiceChannel(s, i)
		if channelID == "" {
			return respondEphemeral(s, i, "Join a voice channel first.")
		}

		playCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		requester := requesterName(i)

		if err := deferChannel(s, i); err != nil {
			return err
		}
		if music.IsPlaylistURL(query) {
			_ = editDeferredEmbed(s, i, loadingPlaylistEmbed(query))
		} else {
			_ = editDeferredEmbed(s, i, loadingEmbed(query))
		}

		if err := svc.EnsureVoice(playCtx, s, botUserID(s), i.GuildID, channelID); err != nil {
			_ = editDeferredEmbed(s, i, voiceErrorEmbed(music.FriendlyControlError(err)))
			return nil
		}

		_ = editDeferredEmbed(s, i, preparingEmbed(query))

		result, err := svc.Enqueue(playCtx, i.GuildID, query, requesterID(i), requester)
		if err != nil {
			_ = editDeferredEmbed(s, i, playErrorEmbed(music.DescribePlaybackError(err)))
			return nil
		}

		if len(result.Tracks) == 0 {
			_ = editDeferredEmbed(s, i, playErrorEmbed(music.DescribePlaybackError(fmt.Errorf("nothing was added to the queue"))))
			return nil
		}

		if result.Started {
			return panel.PublishFromInteraction(s, i, result.Tracks[0], requester)
		}

		panel.UpdateGuildPanel(s, i.GuildID)
		if result.Playlist {
			return editDeferredEmbed(s, i, playlistQueuedEmbed(result.PlaylistTotal, requester))
		}
		return editDeferredEmbed(s, i, queuedEmbed(result.Tracks[0], svc.QueuePosition(i.GuildID), requester))
	}
}

func skipHandler(svc *music.Service) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		_, ok, err := svc.Skip(ctx, i.GuildID)
		if err != nil {
			return respondEphemeral(s, i, music.FriendlyControlError(err))
		}
		if !ok {
			return respondChannelEmbed(s, i, statusEmbed("Skipped", "Queue is empty.", colorQueued))
		}
		return respondEphemeral(s, i, "Skipped to the next track.")
	}
}

func queueHandler(svc *music.Service) Handler {
	return func(_ context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		return respondQueueManage(s, i, svc)
	}
}

func nowPlayingHandler(svc *music.Service, panel *PlayerPanel) Handler {
	return func(_ context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		state := svc.PlaybackState(i.GuildID)
		if state.Now == nil {
			return respondChannelEmbed(s, i, idleEmbed())
		}

		if err := deferChannel(s, i); err != nil {
			return err
		}

		track := queueItemToTrack(*state.Now)
		return panel.PublishFromInteraction(s, i, track, state.Now.RequesterName)
	}
}

func pauseHandler(svc *music.Service) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		if err := svc.Pause(ctx, i.GuildID); err != nil {
			return respondEphemeral(s, i, music.FriendlyControlError(err))
		}
		return respondChannelEmbed(s, i, statusEmbed("Paused", "Use `/resume` or the player buttons to continue.", colorPaused))
	}
}

func resumeHandler(svc *music.Service) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		if err := svc.Resume(ctx, i.GuildID); err != nil {
			return respondEphemeral(s, i, music.FriendlyControlError(err))
		}
		return respondChannelEmbed(s, i, statusEmbed("Resumed", "Playback continued.", colorNowPlaying))
	}
}

func playAutocomplete(svc *music.Service) AutocompleteHandler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		query := focusedOptionString(i, "query")
		if query == "" {
			return respondAutocomplete(s, i, nil)
		}

		// Discord drops autocomplete responses after ~3s; leave room for the round trip.
		searchCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		defer cancel()

		results, err := svc.Search(searchCtx, query)
		if err != nil || len(results) == 0 {
			return respondAutocomplete(s, i, nil)
		}

		choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(results))
		const searchValuePrefix = "search:"
		maxTitleLen := 100 - len(searchValuePrefix)
		for _, result := range results {
			// A video ID pins the exact pick; searching the title again could
			// land on a different upload than the one shown in the list.
			value := searchValuePrefix + truncateRunes(result.Title, maxTitleLen)
			if result.ID != "" {
				value = "yt:" + result.ID
			}
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  autocompleteLabel(result),
				Value: value,
			})
		}
		return respondAutocomplete(s, i, choices)
	}
}

// autocompleteLabel renders a search hit as "TITLE — detail [3:41]".
func autocompleteLabel(result music.SearchResult) string {
	const maxLabelLen = 100

	suffix := ""
	if duration := music.FormatDuration(result.DurationSec); duration != "" {
		suffix = " [" + duration + "]"
	}
	label := formatAutocompleteChoice(result.Title)
	return truncateRunes(label, maxLabelLen-len([]rune(suffix))) + suffix
}

func formatAutocompleteChoice(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}

	label := title
	for _, sep := range []string{" - ", " – ", " — ", " | "} {
		if parts := strings.SplitN(title, sep, 2); len(parts) == 2 {
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			if len(left) > len(right) {
				label = fmt.Sprintf("%s — %s", strings.ToUpper(music.CleanDisplayTitle(left)), right)
			} else {
				label = fmt.Sprintf("%s — %s", strings.ToUpper(music.CleanDisplayTitle(right)), left)
			}
			break
		}
	}

	return truncateRunes(label, 100)
}

func focusedOptionString(i *discordgo.InteractionCreate, name string) string {
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == name && opt.Focused {
			return opt.StringValue()
		}
	}
	return ""
}

func optionString(i *discordgo.InteractionCreate, name string) string {
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}

func botUserID(s *discordgo.Session) string {
	if s != nil && s.State.User != nil {
		return s.State.User.ID
	}
	return ""
}

func requesterID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func respondChannel(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
}
