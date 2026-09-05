package commands

import (
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/music"
)

const queueManageLimit = 25

func queueManageEmbed(snapshot music.QueueSnapshot) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Color: colorQueueList,
		Author: &discordgo.MessageEmbedAuthor{
			Name: "QUEUE MANAGER",
		},
	}

	if snapshot.Now == nil && len(snapshot.Upcoming) == 0 {
		embed.Description = queueEmptyDescription()
		return withSiteFooter(embed, "")
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
			Value:  formatUpNextList(snapshot.Upcoming, queueManageLimit),
			Inline: false,
		})
	}

	remaining := len(snapshot.Upcoming) - queueManageLimit
	extra := "Use the menus below to remove tracks or bump one to play next."
	if remaining > 0 {
		extra = fmt.Sprintf("+%d more · use menus below for the first %d", remaining, queueManageLimit)
	} else if len(snapshot.Upcoming) == 0 {
		extra = ""
	}
	return withSiteFooter(embed, extra)
}

func queueManageComponents(guildID string, upcoming []music.QueueItem) []discordgo.MessageComponent {
	if len(upcoming) == 0 {
		return nil
	}

	limit := len(upcoming)
	if limit > queueManageLimit {
		limit = queueManageLimit
	}

	removeOptions := make([]discordgo.SelectMenuOption, 0, limit)
	boostOptions := make([]discordgo.SelectMenuOption, 0, limit)
	for i := 0; i < limit; i++ {
		item := upcoming[i]
		title := cleanDisplayTitle(item.Title)
		if title == "" {
			title = "Unknown track"
		}
		label := truncateRunes(fmt.Sprintf("#%d %s", i+1, title), 100)
		value := strconv.Itoa(i + 1)
		removeOptions = append(removeOptions, discordgo.SelectMenuOption{
			Label: label,
			Value: value,
		})
		boostOptions = append(boostOptions, discordgo.SelectMenuOption{
			Label: label,
			Value: value,
		})
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				CustomID:    "music:qremove:" + guildID,
				Placeholder: "Remove from queue…",
				Options:     removeOptions,
			},
		}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				CustomID:    "music:qboost:" + guildID,
				Placeholder: "Play next…",
				Options:     boostOptions,
			},
		}},
	}
}

func respondQueueManage(s *discordgo.Session, i *discordgo.InteractionCreate, svc *music.Service) error {
	snapshot := svc.Snapshot(i.GuildID)
	embed := queueManageEmbed(snapshot)
	components := queueManageComponents(i.GuildID, snapshot.Upcoming)
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	})
}

func editQueueManage(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	embeds := []*discordgo.MessageEmbed{embed}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &components,
	})
	return err
}

func playlistQueuedEmbed(total int, requester string) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Color: colorQueued,
		Author: &discordgo.MessageEmbedAuthor{
			Name: "PLAYLIST ADDED",
		},
		Description: fmt.Sprintf("Queued **%d tracks** from the playlist.", total),
	}
	extra := ""
	if requester != "" {
		extra = "Requested by " + requester
	}
	return withSiteFooter(embed, extra)
}

func loadingPlaylistEmbed(query string) *discordgo.MessageEmbed {
	return withSiteFooter(&discordgo.MessageEmbed{
		Color: colorLoading,
		Author: &discordgo.MessageEmbedAuthor{
			Name: "LOADING PLAYLIST",
		},
		Description: fmt.Sprintf(
			"**%s**\n\nPulling up to **%d tracks** into the queue…\n\n*This can take a little longer than a single song.*",
			formatLoadingQuery(query),
			music.MaxPlaylistTracks,
		),
	}, "")
}
