package commands

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/music"
)

const playerComponentPrefix = "music:"

// PlayerPanel keeps a single now-playing message per guild with control buttons.
type PlayerPanel struct {
	logger *slog.Logger
	svc    *music.Service

	mu    sync.Mutex
	panel map[string]panelRef
}

type panelRef struct {
	ChannelID string
	MessageID string
}

// NewPlayerPanel creates a panel manager wired to playback events.
func NewPlayerPanel(logger *slog.Logger, svc *music.Service) *PlayerPanel {
	p := &PlayerPanel{
		logger: logger,
		svc:    svc,
		panel:  make(map[string]panelRef),
	}
	svc.SetPlaybackListener(p.onPlaybackEvent)
	return p
}

// PublishFromInteraction sets the now-playing panel from a slash command response.
func (p *PlayerPanel) PublishFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, track music.ResolvedTrack, requester string) error {
	state := p.svc.PlaybackState(i.GuildID)
	embed := nowPlayingEmbed(track, requester, state)
	components := playerControlsForGuild(i.GuildID, state)

	msg, err := editDeferredPlayer(s, i, embed, components)
	if err != nil {
		return err
	}
	if msg != nil {
		p.setRef(i.GuildID, msg.ChannelID, msg.ID)
	}
	return nil
}

// HandleComponent routes music control button presses.
func (p *PlayerPanel) HandleComponent(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.GuildID == "" {
		return respondEphemeral(s, i, "Music controls only work in a server.")
	}

	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, playerComponentPrefix) {
		return respondEphemeral(s, i, "Unknown control.")
	}

	action := componentAction(customID)
	switch action {
	case "queue":
		return respondEphemeralEmbed(s, i, queueListEmbed(p.svc.Snapshot(i.GuildID)))
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		return err
	}

	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var err error
	switch action {
	case "pause":
		err = p.svc.Pause(opCtx, i.GuildID)
	case "resume":
		err = p.svc.Resume(opCtx, i.GuildID)
	case "skip":
		_, _, err = p.svc.Skip(opCtx, i.GuildID)
	case "stop":
		err = p.svc.Leave(opCtx, s, botUserID(s), i.GuildID)
	default:
		_ = respondComponentError(s, i, "Unknown control.")
		return nil
	}

	if err != nil {
		return p.handleControlError(s, i, i.GuildID, err)
	}

	if action == "stop" {
		return p.renderIdle(s, i)
	}

	return p.refreshPanel(s, i, i.GuildID)
}

// UpdateGuildPanel refreshes the stored now-playing message for a guild, if any.
func (p *PlayerPanel) UpdateGuildPanel(s *discordgo.Session, guildID string) {
	ref, ok := p.getRef(guildID)
	if !ok {
		return
	}

	state := p.svc.PlaybackState(guildID)
	if state.Now == nil {
		_ = p.editMessage(s, ref, idleEmbed(), nil)
		p.clearRef(guildID)
		return
	}

	track := queueItemToTrack(*state.Now)
	embed := nowPlayingEmbed(track, state.Now.RequesterName, state)
	components := playerControlsForGuild(guildID, state)
	_ = p.editMessage(s, ref, embed, components)
}

func (p *PlayerPanel) handleControlError(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string, err error) error {
	if refreshErr := p.refreshPanel(s, i, guildID); refreshErr != nil {
		p.logger.Warn("refreshing panel after control error", "error", refreshErr)
	}
	_, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: music.FriendlyControlError(err),
	})
	return followErr
}

func (p *PlayerPanel) onPlaybackEvent(event music.PlaybackEvent) {
	if event.GuildID == "" || event.Session == nil {
		return
	}

	ref, ok := p.getRef(event.GuildID)
	if !ok {
		return
	}

	state := p.svc.PlaybackState(event.GuildID)
	if state.Now == nil {
		_ = p.editMessage(event.Session, ref, idleEmbed(), nil)
		p.clearRef(event.GuildID)
		return
	}

	track := queueItemToTrack(*state.Now)
	embed := nowPlayingEmbed(track, state.Now.RequesterName, state)
	components := playerControlsForGuild(event.GuildID, state)
	_ = p.editMessage(event.Session, ref, embed, components)
}

func (p *PlayerPanel) refreshPanel(s *discordgo.Session, i *discordgo.InteractionCreate, guildID string) error {
	state := p.svc.PlaybackState(guildID)
	if state.Now == nil {
		return p.renderIdle(s, i)
	}

	track := queueItemToTrack(*state.Now)
	embed := nowPlayingEmbed(track, state.Now.RequesterName, state)
	components := playerControlsForGuild(guildID, state)

	embeds := []*discordgo.MessageEmbed{embed}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &components,
	})
	return err
}

func (p *PlayerPanel) renderIdle(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	p.clearRef(i.GuildID)
	embeds := []*discordgo.MessageEmbed{idleEmbed()}
	empty := []discordgo.MessageComponent{}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &empty,
	})
	return err
}

func (p *PlayerPanel) editMessage(s *discordgo.Session, ref panelRef, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    ref.ChannelID,
		ID:         ref.MessageID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
	return err
}

func (p *PlayerPanel) setRef(guildID, channelID, messageID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.panel[guildID] = panelRef{ChannelID: channelID, MessageID: messageID}
}

func (p *PlayerPanel) getRef(guildID string) (panelRef, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ref, ok := p.panel[guildID]
	return ref, ok
}

func (p *PlayerPanel) clearRef(guildID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.panel, guildID)
}

func componentAction(customID string) string {
	parts := strings.Split(customID, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func queueItemToTrack(item music.QueueItem) music.ResolvedTrack {
	return music.ResolvedTrack{
		Title:       item.Title,
		Artist:      item.Artist,
		Thumbnail:   item.Thumbnail,
		PageURL:     item.PageURL,
		DurationSec: item.DurationSec,
	}
}

func respondComponentError(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) error {
	content := msg
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	return err
}

func editDeferredPlayer(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	embeds := []*discordgo.MessageEmbed{embed}
	return s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &components,
	})
}
