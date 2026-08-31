package music

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/linkdave"
)

// PlaybackEvent notifies UI layers about queue and transport changes.
type PlaybackEvent struct {
	GuildID      string
	Session      *discordgo.Session
	RefreshOnly  bool // true for pause/resume; false when the active track changes or stops
}

// PlaybackState is a snapshot of the current guild player for UI rendering.
type PlaybackState struct {
	Now            *QueueItem
	Upcoming       int
	Paused         bool
	ElapsedSec     int
	VoteSkipCount  int
	VoteSkipNeeded int
}

// Service coordinates Linkdave playback and per-guild queues.
type Service struct {
	logger   *slog.Logger
	linkdave *linkdave.Client
	resolver *Resolver

	mu     sync.Mutex
	queues map[string]*Queue

	listenerMu sync.Mutex
	listener   func(PlaybackEvent)
	session    *discordgo.Session

	voteMu   sync.Mutex
	voteSkip map[string]map[string]struct{}

	clockMu       sync.Mutex
	playbackStart map[string]time.Time
	frozenElapsed map[string]int
}

// NewService creates a music service.
func NewService(logger *slog.Logger, client *linkdave.Client, resolver *Resolver) *Service {
	svc := &Service{
		logger:        logger,
		linkdave:      client,
		resolver:      resolver,
		queues:        make(map[string]*Queue),
		playbackStart: make(map[string]time.Time),
		frozenElapsed: make(map[string]int),
	}

	for _, guildID := range client.GuildIDs() {
		svc.wireGuild(guildID)
	}
	return svc
}

// SetPlaybackListener registers a callback for queue and transport updates.
func (s *Service) SetPlaybackListener(fn func(PlaybackEvent)) {
	s.listenerMu.Lock()
	s.listener = fn
	s.listenerMu.Unlock()
}

// SetSession stores the Discord session used for background UI updates.
func (s *Service) SetSession(session *discordgo.Session) {
	s.listenerMu.Lock()
	s.session = session
	s.listenerMu.Unlock()
}

// PlaybackState returns the current guild player snapshot for UI rendering.
func (s *Service) PlaybackState(guildID string) PlaybackState {
	player := s.linkdave.Player(guildID)
	now, upcoming := s.queue(guildID).Snapshot()

	state := PlaybackState{
		Upcoming: len(upcoming),
		Paused:   player.State() == linkdave.PlayerStatePaused || s.isPlaybackPaused(guildID),
	}
	if now != nil {
		copy := *now
		state.Now = &copy
	}
	state.ElapsedSec = s.elapsedSec(guildID)

	s.listenerMu.Lock()
	session := s.session
	s.listenerMu.Unlock()
	if session != nil && session.State != nil && session.State.User != nil {
		state.VoteSkipCount, state.VoteSkipNeeded = s.VoteSkipStatus(session, session.State.User.ID, guildID)
	}
	return state
}

// HasActivePlayback reports whether the guild already has a track playing or queued.
func (s *Service) HasActivePlayback(guildID string) bool {
	player := s.linkdave.Player(guildID)
	now, upcoming := s.queue(guildID).Snapshot()
	if now != nil || len(upcoming) > 0 {
		return true
	}
	switch player.State() {
	case linkdave.PlayerStatePlaying, linkdave.PlayerStatePaused:
		return true
	default:
		return s.queue(guildID).IsActive()
	}
}

func (s *Service) markPlaybackStart(guildID string) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	s.playbackStart[guildID] = time.Now()
	delete(s.frozenElapsed, guildID)
	s.clearVoteSkip(guildID)
}

func (s *Service) markPlaybackPause(guildID string) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	if start, ok := s.playbackStart[guildID]; ok {
		s.frozenElapsed[guildID] = int(time.Since(start).Seconds())
	}
}

func (s *Service) markPlaybackResume(guildID string) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	if elapsed, ok := s.frozenElapsed[guildID]; ok {
		s.playbackStart[guildID] = time.Now().Add(-time.Duration(elapsed) * time.Second)
		delete(s.frozenElapsed, guildID)
	}
}

func (s *Service) clearPlaybackClock(guildID string) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	delete(s.playbackStart, guildID)
	delete(s.frozenElapsed, guildID)
}

func (s *Service) elapsedSec(guildID string) int {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	if elapsed, ok := s.frozenElapsed[guildID]; ok {
		return elapsed
	}
	if start, ok := s.playbackStart[guildID]; ok {
		return int(time.Since(start).Seconds())
	}
	return 0
}

func (s *Service) isPlaybackPaused(guildID string) bool {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	_, ok := s.frozenElapsed[guildID]
	return ok
}

func (s *Service) emitPlayback(guildID string, refreshOnly bool) {
	s.listenerMu.Lock()
	fn := s.listener
	session := s.session
	s.listenerMu.Unlock()
	if fn == nil {
		return
	}
	fn(PlaybackEvent{GuildID: guildID, Session: session, RefreshOnly: refreshOnly})
}
func (s *Service) Join(ctx context.Context, session *discordgo.Session, botUserID, guildID, channelID string) error {
	if channelID == "" {
		return fmt.Errorf("join a voice channel first")
	}
	return s.ensureVoice(ctx, session, botUserID, guildID, channelID)
}

// Leave disconnects voice and clears the guild queue.
func (s *Service) Leave(ctx context.Context, session *discordgo.Session, botUserID, guildID string) error {
	s.queue(guildID).Clear()
	s.clearPlaybackClock(guildID)
	s.clearVoteSkip(guildID)
	player := s.linkdave.Player(guildID)

	botChannel := BotVoiceChannel(session, guildID, botUserID)
	if botChannel != "" || player.Connected() {
		if err := player.Disconnect(ctx); err != nil {
			return err
		}
	}
	s.emitPlayback(guildID, false)
	return nil
}

// EnsureVoice joins or re-syncs the bot voice session with Linkdave.
func (s *Service) EnsureVoice(ctx context.Context, session *discordgo.Session, botUserID, guildID, channelID string) error {
	return s.ensureVoice(ctx, session, botUserID, guildID, channelID)
}

func (s *Service) ensureVoice(ctx context.Context, session *discordgo.Session, botUserID, guildID, channelID string) error {
	player := s.linkdave.Player(guildID)
	s.wireGuild(guildID)

	if player.Connected() && player.ChannelID() == channelID {
		return nil
	}

	botChannel := BotVoiceChannel(session, guildID, botUserID)
	if botChannel != "" && (botChannel != channelID || !player.Connected()) {
		// Discord still shows the bot in voice after a restart, but Linkdave lost state.
		// Leave first so Discord sends fresh voice server credentials.
		s.logger.Info("resetting stale voice session", "guild_id", guildID, "discord_channel", botChannel)
		_ = session.ChannelVoiceJoinManual(guildID, "", false, false)
		player.ResetVoice()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(750 * time.Millisecond):
		}
	}

	if player.Connected() {
		if err := player.Disconnect(ctx); err != nil {
			return fmt.Errorf("leaving previous voice channel: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	if err := player.Connect(ctx, channelID); err != nil {
		return err
	}
	return nil
}

// Skip stops the current track and plays the next queued item when present.
func (s *Service) Skip(ctx context.Context, guildID string) (QueueItem, bool, error) {
	player := s.linkdave.Player(guildID)
	queue := s.queue(guildID)

	next, ok := queue.Skip()
	if !ok {
		if err := player.Stop(ctx); err != nil {
			return QueueItem{}, false, err
		}
		queue.Clear()
		s.clearPlaybackClock(guildID)
		s.emitPlayback(guildID, false)
		return QueueItem{}, false, nil
	}

	if err := player.Play(ctx, next.StreamURL, next.RequesterID); err != nil {
		return QueueItem{}, false, err
	}
	s.markPlaybackStart(guildID)
	s.emitPlayback(guildID, false)
	return next, true, nil
}

// Pause pauses playback for a guild.
func (s *Service) Pause(ctx context.Context, guildID string) error {
	if err := s.linkdave.Player(guildID).Pause(ctx); err != nil {
		return err
	}
	s.markPlaybackPause(guildID)
	s.emitPlayback(guildID, true)
	return nil
}

// Resume resumes playback for a guild.
func (s *Service) Resume(ctx context.Context, guildID string) error {
	if err := s.linkdave.Player(guildID).Resume(ctx); err != nil {
		return err
	}
	s.markPlaybackResume(guildID)
	s.emitPlayback(guildID, true)
	return nil
}

// Search returns YouTube matches for slash-command autocomplete.
func (s *Service) Search(ctx context.Context, query string) ([]SearchResult, error) {
	return s.resolver.Search(ctx, query, 5)
}

// QueueSnapshot is a copy of the guild queue for UI rendering.
type QueueSnapshot struct {
	Now      *QueueItem
	Upcoming []QueueItem
}

// Snapshot returns queue state for display embeds.
func (s *Service) Snapshot(guildID string) QueueSnapshot {
	now, upcoming := s.queue(guildID).Snapshot()
	snap := QueueSnapshot{Upcoming: upcoming}
	if now != nil {
		copy := *now
		snap.Now = &copy
	}
	return snap
}

// QueuePosition returns the 1-based queue position for a newly enqueued track.
func (s *Service) QueuePosition(guildID string) int {
	return s.queue(guildID).Len()
}

// QueueText formats the guild queue for display.
func (s *Service) QueueText(guildID string) string {
	now, upcoming := s.queue(guildID).Snapshot()
	if now == nil && len(upcoming) == 0 {
		return "Queue is empty."
	}

	var b strings.Builder
	if now != nil {
		fmt.Fprintf(&b, "Now playing: **%s**\n", now.Title)
	}
	if len(upcoming) == 0 {
		return strings.TrimSpace(b.String())
	}

	b.WriteString("Up next:\n")
	for i, item := range upcoming {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item.Title)
	}
	return strings.TrimSpace(b.String())
}

// BotVoiceChannel returns the voice channel the bot is currently in, if any.
func BotVoiceChannel(s *discordgo.Session, guildID, botUserID string) string {
	if s == nil || guildID == "" || botUserID == "" {
		return ""
	}
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return ""
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == botUserID {
			return vs.ChannelID
		}
	}
	return ""
}
func MemberVoiceChannel(s *discordgo.Session, i *discordgo.InteractionCreate) string {
	if i == nil || i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		return ""
	}

	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		return ""
	}

	for _, vs := range guild.VoiceStates {
		if vs.UserID == i.Member.User.ID {
			return vs.ChannelID
		}
	}
	return ""
}

func (s *Service) resetStaleQueue(guildID string) {
	player := s.linkdave.Player(guildID)
	queue := s.queue(guildID)
	if !queue.IsActive() {
		return
	}
	if player.State() == linkdave.PlayerStatePlaying || player.State() == linkdave.PlayerStatePaused {
		return
	}
	queue.Clear()
	s.clearPlaybackClock(guildID)
}

func (s *Service) queue(guildID string) *Queue {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q, ok := s.queues[guildID]; ok {
		return q
	}
	q := NewQueue()
	s.queues[guildID] = q
	return q
}

func (s *Service) wireGuild(guildID string) {
	player := s.linkdave.Player(guildID)
	player.SetTrackEndHook(func(data linkdave.TrackEndData) {
		if data.Reason == linkdave.TrackEndStopped || data.Reason == linkdave.TrackEndReplaced {
			return
		}
		s.onTrackEnd(guildID, data.Reason)
	})
}

func (s *Service) onTrackEnd(guildID, reason string) {
	if reason == linkdave.TrackEndStopped || reason == linkdave.TrackEndReplaced {
		return
	}

	queue := s.queue(guildID)
	if !queue.IsActive() {
		return
	}

	next, ok := queue.Advance()
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	player := s.linkdave.Player(guildID)
	if err := player.Play(ctx, next.StreamURL, next.RequesterID); err != nil {
		s.logger.Error("playing next queued track", "guild_id", guildID, "error", err)
		queue.Clear()
		s.clearPlaybackClock(guildID)
	} else {
		s.markPlaybackStart(guildID)
		s.emitPlayback(guildID, false)
	}
}
