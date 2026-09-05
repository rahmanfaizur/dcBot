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
func AICommands(client *ai.Client, svc *music.Service, panel *PlayerPanel) []Command {
	if client == nil || !client.Enabled() {
		return nil
	}
	var store *SuggestStore
	if panel != nil {
		store = panel.SuggestStore()
	} else {
		store = NewSuggestStore()
	}
	return []Command{
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "ask",
				Description: "Ask FR anything — music picks come with play buttons.",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "prompt",
						Description: "Your question or prompt",
						Required:    true,
					},
				},
			},
			Handler: askHandler(client, store),
		},
		{
			Definition: &discordgo.ApplicationCommand{
				Name:        "vibe",
				Description: "AI vibe check / recs about what's playing (with play buttons).",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "prompt",
						Description: "Optional — e.g. roast the queue, what next, chill kpop",
						Required:    false,
					},
				},
			},
			Handler: vibeHandler(client, svc, store),
		},
	}
}

func askHandler(client *ai.Client, store *SuggestStore) Handler {
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
		aiCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		reply, err := client.Suggest(aiCtx, askSuggestSystem(), prompt)
		if err != nil {
			slog.Warn("groq ask failed", "error", err)
			return editDeferredEphemeral(s, i, "AI is taking a nap — try again in a moment.")
		}
		return respondAIReply(s, i, "ASK", reply, store)
	}
}

func vibeHandler(client *ai.Client, svc *music.Service, store *SuggestStore) Handler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
		prompt := strings.TrimSpace(optionString(i, "prompt"))
		if prompt == "" {
			prompt = "Give a short vibe check on what's playing (or the queue). Suggest what to play next."
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

		user := prompt
		if contextBits.Len() > 0 {
			user = contextBits.String() + "\nUser: " + prompt
		}

		aiCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		reply, err := client.Suggest(aiCtx, vibeSuggestSystem(), user)
		if err != nil {
			slog.Warn("groq vibe failed", "error", err)
			return editDeferredEphemeral(s, i, "Couldn't vibe right now — try again shortly.")
		}
		return respondAIReply(s, i, "VIBE", reply, store)
	}
}

func aiEmbed(title, body string) *discordgo.MessageEmbed {
	return withSiteFooter(&discordgo.MessageEmbed{
		Color:       colorFunFact,
		Author:      &discordgo.MessageEmbedAuthor{Name: title},
		Description: body,
	}, "Powered by Groq · FR Music")
}
