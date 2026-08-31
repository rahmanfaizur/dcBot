// Package commands registers and dispatches Discord slash commands.
package commands

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// Handler processes a single slash command interaction.
type Handler func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error

// AutocompleteHandler serves slash-command autocomplete requests.
type AutocompleteHandler func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error

// Command describes a slash command and its handler.
type Command struct {
	Definition   *discordgo.ApplicationCommand
	Handler      Handler
	Autocomplete AutocompleteHandler
}

// Registry maps command names to their handlers.
type Registry struct {
	logger           *slog.Logger
	commands         []Command
	byName           map[string]Handler
	byAutocomplete   map[string]AutocompleteHandler
	componentHandler ComponentHandler
}

// ComponentHandler processes message component interactions such as buttons.
type ComponentHandler func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error

// NewRegistry builds an empty command registry.
func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		logger:         logger,
		byName:         make(map[string]Handler),
		byAutocomplete: make(map[string]AutocompleteHandler),
	}
}

// Register adds a command to the registry. Register panics on duplicate names
// because that indicates a programming error during startup.
func (r *Registry) Register(cmd Command) {
	if cmd.Definition == nil {
		panic("commands.Register: Definition is nil")
	}
	if cmd.Handler == nil {
		panic(fmt.Sprintf("commands.Register: Handler is nil for /%s", cmd.Definition.Name))
	}
	if _, exists := r.byName[cmd.Definition.Name]; exists {
		panic(fmt.Sprintf("commands.Register: duplicate command /%s", cmd.Definition.Name))
	}

	r.commands = append(r.commands, cmd)
	r.byName[cmd.Definition.Name] = cmd.Handler
	if cmd.Autocomplete != nil {
		r.byAutocomplete[cmd.Definition.Name] = cmd.Autocomplete
	}
}

// SetComponentHandler registers a handler for button and menu interactions.
func (r *Registry) SetComponentHandler(handler ComponentHandler) {
	r.componentHandler = handler
}

// All returns registered command definitions for Discord API registration.
func (r *Registry) All() []*discordgo.ApplicationCommand {
	defs := make([]*discordgo.ApplicationCommand, len(r.commands))
	for i, cmd := range r.commands {
		defs[i] = cmd.Definition
	}
	return defs
}

// HandleInteraction routes slash commands and autocomplete requests.
func (r *Registry) HandleInteraction(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		r.handleCommand(ctx, s, i)
	case discordgo.InteractionApplicationCommandAutocomplete:
		r.handleAutocomplete(ctx, s, i)
	case discordgo.InteractionMessageComponent:
		r.handleComponent(ctx, s, i)
	}
}

func (r *Registry) handleComponent(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
	if r.componentHandler == nil {
		_ = respondEphemeral(s, i, "This control is not available.")
		return
	}
	if err := r.componentHandler(ctx, s, i); err != nil {
		r.logger.Error("component interaction failed", "error", err)
		_ = respondEphemeral(s, i, "Something went wrong with that control.")
	}
}

func (r *Registry) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := i.ApplicationCommandData().Name
	handler, ok := r.byName[name]
	if !ok {
		r.logger.Warn("unhandled slash command", "command", name)
		_ = respondEphemeral(s, i, "Unknown command.")
		return
	}

	if err := handler(ctx, s, i); err != nil {
		r.logger.Error("command failed", "command", name, "error", err)
		_ = respondEphemeral(s, i, "Something went wrong running that command.")
	}
}

func (r *Registry) handleAutocomplete(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := i.ApplicationCommandData().Name
	handler, ok := r.byAutocomplete[name]
	if !ok {
		_ = respondAutocomplete(s, i, nil)
		return
	}

	if err := handler(ctx, s, i); err != nil {
		r.logger.Warn("autocomplete failed", "command", name, "error", err)
		_ = respondAutocomplete(s, i, nil)
	}
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func respondAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate, choices []*discordgo.ApplicationCommandOptionChoice) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

// deferChannel acknowledges a slow command so Discord does not expire the interaction.
func deferChannel(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
}

func followupChannel(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Content: content,
	})
	return err
}

func editDeferredEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	return err
}

func editDeferredEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	embeds := []*discordgo.MessageEmbed{embed}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &embeds,
	})
	return err
}

func respondEphemeralEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func respondChannelEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

func followupChannelEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	_, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	return err
}

func followupEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: content,
	})
	return err
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
