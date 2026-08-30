package commands

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

// PingCommand returns the /ping health-check command.
func PingCommand() Command {
	return Command{
		Definition: &discordgo.ApplicationCommand{
			Name:        "ping",
			Description: "Check whether the bot is online.",
		},
		Handler: handlePing,
	}
}

func handlePing(_ context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return respondEphemeral(s, i, "Pong!")
}
