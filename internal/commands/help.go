package commands

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

// HelpCommand returns /help with command list and examples.
func HelpCommand() Command {
	return Command{
		Definition: &discordgo.ApplicationCommand{
			Name:        "help",
			Description: "Show FR Music commands with examples.",
		},
		Handler: handleHelp,
	}
}

func handleHelp(_ context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	embed := withSiteFooter(&discordgo.MessageEmbed{
		Color:  colorNowPlaying,
		Author: &discordgo.MessageEmbedAuthor{Name: "FR MUSIC · HELP"},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Music",
				Value: "`/play query:` `chill kpop` — search, YouTube/Spotify, playlist\n" +
					"`/nowplaying` — player panel\n" +
					"`/queue` — upcoming; remove / play-next menus\n" +
					"`/skip` · `/pause` · `/resume`\n" +
					"`/shuffle` — mix upcoming\n" +
					"`/loop mode:` `track` | `queue` | `off`\n" +
					"`/clear` — stop + empty queue\n" +
					"`/join` · `/leave`",
			},
			{
				Name: "AI",
				Value: "`/ask prompt:` `play chill kpop songs` — chat; picks get **play buttons**\n" +
					"`/vibe prompt:` `what next?` — vibe check + optional buttons\n" +
					"Join voice, then tap ▶ under the reply to play.",
			},
			{
				Name:  "Other",
				Value: "`/ping` — health check\n`/help` — this message\nhttps://music.frlabs.me",
			},
		},
	}, "Soft nights, shared voice channels")
	return respondChannelEmbed(s, i, embed)
}
