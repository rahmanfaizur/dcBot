package commands

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/ai"
	"github.com/faizur/mybot/internal/music"
)

const playerComponentPrefix = "music:"

// PlayerPanel keeps a single now-playing message per guild with control buttons.
type PlayerPanel struct {
	logger *slog.Logger
	svc    *music.Service
	ai     *ai.Client

	mu    sync.Mutex
	panel map[string]panelRef
}

type panelRef struct {
	ChannelID string
	MessageID string
}

// NewPlayerPanel creates a panel manager wired to playback events.
func NewPlayerPanel(logger *slog.Logger, svc *music.Service, aiClient *ai.Client) *PlayerPanel {
	p := &PlayerPanel{
		logger: logger,
		svc:    svc,
		ai:     aiClient,
		panel:  make(map[string]panelRef),
	}
	svc.SetPlaybackListener(p.onPlaybackEvent)
	return p
}

// PublishFromInteraction sets the now-playing panel from a slash command response.
func (p *PlayerPanel) PublishFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, track music.ResolvedTrack, requester string) error {
	p.stripOldPanelButtons(s, i.GuildID)

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
		return respondQueueManage(s, i, p.svc)
	case "fact":
		state := p.svc.PlaybackState(i.GuildID)
		if state.Now == nil {
			return respondEphemeral(s, i, "Nothing is playing right now.")
		}
		if err := deferEphemeral(s, i); err != nil {
			return err
		}
		factCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		track := queueItemToTrack(*state.Now)
		return editDeferredEmbed(s, i, songFactEmbed(factCtx, track, state.Now.RequesterName, p.ai))
	case "qremove", "qboost":
		return p.handleQueueEdit(ctx, s, i, action)
	case "voteskip":
		state := p.svc.PlaybackState(i.GuildID)
		if state.Now == nil {
			return respondEphemeral(s, i, "Nothing is playing right now.")
		}
		if err := deferEphemeral(s, i); err != nil {
			return err
		}
		opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		current, needed, skipped, err := p.svc.VoteSkip(opCtx, s, botUserID(s), i.GuildID, requesterID(i))
		if err != nil {
			_, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: music.FriendlyControlError(err),
			})
			return followErr
		}
		if skipped {
			p.UpdateGuildPanel(s, i.GuildID)
			return editDeferredEphemeral(s, i, fmt.Sprintf("Vote skip passed (%d/%d). Skipped to the next track.", needed, needed))
		}
		p.UpdateGuildPanel(s, i.GuildID)
		return editDeferredEphemeral(s, i, fmt.Sprintf("Vote recorded **%d/%d** to skip.", current, needed))
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
		if err != nil {
			return p.handleControlError(s, i, i.GuildID, err)
		}
		return p.acknowledgeControl(s, i)
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

	state := p.svc.PlaybackState(event.GuildID)
	if state.Now == nil {
		ref, ok := p.getRef(event.GuildID)
		if ok {
			_ = p.editMessage(event.Session, ref, idleEmbed(), nil)
		}
		p.clearRef(event.GuildID)
		return
	}

	if event.RefreshOnly {
		p.UpdateGuildPanel(event.Session, event.GuildID)
		return
	}

	ref, ok := p.getRef(event.GuildID)
	if !ok {
		return
	}

	track := queueItemToTrack(*state.Now)
	_ = p.publishFreshPanel(event.Session, event.GuildID, ref.ChannelID, track, state.Now.RequesterName, state)
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

func (p *PlayerPanel) stripOldPanelButtons(s *discordgo.Session, guildID string) {
	ref, ok := p.getRef(guildID)
	if !ok {
		return
	}
	empty := []discordgo.MessageComponent{}
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    ref.ChannelID,
		ID:         ref.MessageID,
		Components: &empty,
	}); err != nil {
		p.logger.Warn("stripping old panel buttons", "guild_id", guildID, "error", err)
	}
}

func (p *PlayerPanel) publishFreshPanel(s *discordgo.Session, guildID, channelID string, track music.ResolvedTrack, requester string, state music.PlaybackState) error {
	p.stripOldPanelButtons(s, guildID)

	embed := nowPlayingEmbed(track, requester, state)
	components := playerControlsForGuild(guildID, state)
	msg, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		p.logger.Warn("posting fresh player panel", "guild_id", guildID, "error", err)
		return err
	}
	p.setRef(guildID, channelID, msg.ID)
	return nil
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

func (p *PlayerPanel) acknowledgeControl(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	empty := []discordgo.MessageComponent{}
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Components: &empty,
	})
	return err
}

func (p *PlayerPanel) handleQueueEdit(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, action string) error {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return respondEphemeral(s, i, "Pick a track first.")
	}
	index, err := strconv.Atoi(data.Values[0])
	if err != nil || index < 1 {
		return respondEphemeral(s, i, "Invalid queue position.")
	}

	var status string
	switch action {
	case "qremove":
		removed, ok := p.svc.RemoveQueueItem(i.GuildID, index)
		if !ok {
			return respondEphemeral(s, i, "That queue position is gone.")
		}
		status = fmt.Sprintf("Removed **%s**.", cleanDisplayTitle(removed.Title))
	case "qboost":
		moved, ok := p.svc.MoveQueueToFront(i.GuildID, index)
		if !ok {
			return respondEphemeral(s, i, "That queue position is gone.")
		}
		status = fmt.Sprintf("**%s** will play next.", cleanDisplayTitle(moved.Title))
	default:
		return respondEphemeral(s, i, "Unknown queue action.")
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		return err
	}

	snapshot := p.svc.Snapshot(i.GuildID)
	embed := queueManageEmbed(snapshot)
	embed.Description = status
	components := queueManageComponents(i.GuildID, snapshot.Upcoming)
	return editQueueManage(s, i, embed, components)
}

func editDeferredPlayer(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	embeds := []*discordgo.MessageEmbed{embed}
	return s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &components,
	})
}
