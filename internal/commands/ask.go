package commands

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/ai"
	"github.com/faizur/mybot/internal/music"
)

// AICommands returns slash commands powered by Groq (when configured).
func AICommands(client *ai.Client, svc *music.Service) []Command {
	if client == nil || !client.Enabled() {
		return nil
	}
	return []Command{
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "ask",
				Description: "Ask FR anything (general AI chat).",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "prompt",
						Description: "Your question or prompt",
						Required:    true,
					},
				},
			},
			Handler: askHandler(client),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "vibe",
				Description: "AI chat about the current song / queue (or music in general).",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "prompt",
						Description: "Optional — e.g. roast the queue, what next, vibe check",
						Required:    false,
					},
				},
			},
			Handler: vibeHandler(client, svc),
		},
	}
}

func askHandler(client *ai.Client) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		prompt := strings.TrimSpace(optionString(i, "prompt"))
		if prompt == "" {
			return respondEphemeral(s, i, "Give me something to answer.")
		}
		if ok, wait := client.Allow(requesterID(i)); !ok {
			return respondEphemeral(s, i, fmt.Sprintf("Easy — try again in %s.", wait))
		}
		if err := deferChannel(s, i); err != nil {
			return err
		}
		aiCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		reply, err := client.Chat(aiCtx,
			"You are FR, a warm, witty Discord bot from FR Labs. Keep answers concise (under ~180 words). Be helpful and friendly. Avoid markdown tables.",
			prompt,
		)
		if err != nil {
			slog.Warn("groq ask failed", "error", err)
			return editDeferredEphemeral(s, i, "AI is taking a nap — try again in a moment.")
		}
		return editDeferredEmbed(s, i, aiEmbed("ASK", reply))
	}
}

func vibeHandler(client *ai.Client, svc *music.Service) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		prompt := strings.TrimSpace(optionString(i, "prompt"))
		if prompt == "" {
			prompt = "Give a short vibe check on what's playing (or the queue). Suggest one next track idea."
		}
		if ok, wait := client.Allow(requesterID(i)); !ok {
			return respondEphemeral(s, i, fmt.Sprintf("Easy — try again in %s.", wait))
		}
		if err := deferChannel(s, i); err != nil {
			return err
		}

		var contextBits strings.Builder
		if svc != nil && i.GuildID != "" {
			snap := svc.Snapshot(i.GuildID)
			if snap.Now != nil {
				fmt.Fprintf(&contextBits, "Now playing: %s", snap.Now.Title)
				if snap.Now.Artist != "" {
					fmt.Fprintf(&contextBits, " by %s", snap.Now.Artist)
				}
				contextBits.WriteString("\n")
			} else {
				contextBits.WriteString("Nothing is currently playing.\n")
			}
			if len(snap.Upcoming) > 0 {
				contextBits.WriteString("Up next:\n")
				limit := len(snap.Upcoming)
				if limit > 8 {
					limit = 8
				}
				for idx, item := range snap.Upcoming[:limit] {
					fmt.Fprintf(&contextBits, "%d. %s\n", idx+1, item.Title)
				}
			}
			fmt.Fprintf(&contextBits, "Loop mode: %s\n", music.LoopModeLabel(svc.Loop(i.GuildID)))
		}

		system := "You are FR, a Discord music bot companion. Talk about music vibes, playlists, and rooms. Keep it short and fun (under ~150 words). Do not invent full lyrics."
		user := prompt
		if contextBits.Len() > 0 {
			user = contextBits.String() + "\nUser: " + prompt
		}

		aiCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		reply, err := client.Chat(aiCtx, system, user)
		if err != nil {
			slog.Warn("groq vibe failed", "error", err)
			return editDeferredEphemeral(s, i, "Couldn't vibe right now — try again shortly.")
		}
		return editDeferredEmbed(s, i, aiEmbed("VIBE", reply))
	}
}

func aiEmbed(title, body string) *discordgo.MessageEmbed {
	return withSiteFooter(&discordgo.MessageEmbed{
		Color:       colorFunFact,
		Author:      &discordgo.MessageEmbedAuthor{Name: title},
		Description: body,
	}, "Powered by Groq · FR Music")
}
