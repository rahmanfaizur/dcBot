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
	"github.com/faizur/mybot/internal/store"
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
	Loop           LoopMode
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

	store *store.Store

	leaveMu     sync.Mutex
	leaveTimers map[string]*time.Timer
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
		leaveTimers:   make(map[string]*time.Timer),
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
		Loop:     s.queue(guildID).Loop(),
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

// SetStore attaches Mongo persistence for live website snapshots.
func (s *Service) SetStore(st *store.Store) {
	s.store = st
}

func (s *Service) emitPlayback(guildID string, refreshOnly bool) {
	s.listenerMu.Lock()
	fn := s.listener
	session := s.session
	s.listenerMu.Unlock()
	if fn != nil {
		fn(PlaybackEvent{GuildID: guildID, Session: session, RefreshOnly: refreshOnly})
	}
	s.persistGuild(guildID, session)
	s.syncPresence(session)
}

func (s *Service) persistGuild(guildID string, session *discordgo.Session) {
	if s.store == nil || guildID == "" {
		return
	}

	state := s.PlaybackState(guildID)
	snap := s.Snapshot(guildID)
	doc := store.GuildPlayback{
		GuildID:    guildID,
		Paused:     state.Paused,
		ElapsedSec: state.ElapsedSec,
		Upcoming:   make([]store.Track, 0, len(snap.Upcoming)),
	}
	if !state.Paused {
		if start, ok := s.playbackStartUTC(guildID); ok {
			doc.StartedAt = &start
		}
	}
	if session != nil {
		if g, err := session.State.Guild(guildID); err == nil && g != nil {
			doc.GuildName = g.Name
		}
	}
	if snap.Now != nil {
		t := queueItemToStoreTrack(*snap.Now)
		doc.Now = &t
	}
	for _, item := range snap.Upcoming {
		doc.Upcoming = append(doc.Upcoming, queueItemToStoreTrack(item))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.UpsertGuildPlayback(ctx, doc); err != nil {
		s.logger.Warn("persisting guild playback", "guild_id", guildID, "error", err)
	}
}

func queueItemToStoreTrack(item QueueItem) store.Track {
	return store.Track{
		Title:       item.Title,
		Artist:      item.Artist,
		Thumbnail:   item.Thumbnail,
		PageURL:     item.PageURL,
		DurationSec: item.DurationSec,
		Requester:   item.RequesterName,
	}
}

func (s *Service) playbackStartUTC(guildID string) (time.Time, bool) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	if _, frozen := s.frozenElapsed[guildID]; frozen {
		return time.Time{}, false
	}
	start, ok := s.playbackStart[guildID]
	if !ok {
		return time.Time{}, false
	}
	return start.UTC(), true
}

// Shuffle randomizes the upcoming queue.
func (s *Service) Shuffle(guildID string) (int, error) {
	q := s.queue(guildID)
	if q.Len() < 2 {
		return q.Len(), fmt.Errorf("need at least 2 tracks in the queue to shuffle")
	}
	q.ShuffleUpcoming()
	s.emitPlayback(guildID, true)
	return q.Len(), nil
}

// SetLoop changes the guild loop mode.
func (s *Service) SetLoop(guildID string, mode LoopMode) LoopMode {
	s.queue(guildID).SetLoop(mode)
	s.emitPlayback(guildID, true)
	return mode
}

// Loop returns the guild loop mode.
func (s *Service) Loop(guildID string) LoopMode {
	return s.queue(guildID).Loop()
}

// Clear stops playback and empties the queue but stays in voice.
func (s *Service) Clear(ctx context.Context, guildID string) error {
	s.queue(guildID).Clear()
	s.clearPlaybackClock(guildID)
	s.clearVoteSkip(guildID)
	player := s.linkdave.Player(guildID)
	if player.State() == linkdave.PlayerStatePlaying || player.State() == linkdave.PlayerStatePaused {
		if err := player.Stop(ctx); err != nil {
			return err
		}
	}
	s.emitPlayback(guildID, false)
	return nil
}

// ParseLoopMode maps slash option strings to LoopMode.
func ParseLoopMode(raw string) (LoopMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off", "":
		return LoopOff, nil
	case "track", "song":
		return LoopTrack, nil
	case "queue", "all":
		return LoopQueue, nil
	default:
		return LoopOff, fmt.Errorf("use off, track, or queue")
	}
}

func LoopModeLabel(mode LoopMode) string {
	switch mode {
	case LoopTrack:
		return "track"
	case LoopQueue:
		return "queue"
	default:
		return "off"
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	player := s.linkdave.Player(guildID)

	if queue.Loop() == LoopTrack {
		if current, ok := queue.NowCopy(); ok {
			if err := player.Play(ctx, current.StreamURL, current.RequesterID); err != nil {
				s.logger.Error("looping current track", "guild_id", guildID, "error", err)
				queue.Clear()
				s.clearPlaybackClock(guildID)
				s.emitPlayback(guildID, false)
				return
			}
			s.markPlaybackStart(guildID)
			s.emitPlayback(guildID, false)
			return
		}
	}

	if queue.Loop() == LoopQueue {
		if finished, ok := queue.NowCopy(); ok {
			queue.Enqueue(finished)
		}
	}

	next, ok := queue.Advance()
	if !ok {
		s.clearPlaybackClock(guildID)
		s.emitPlayback(guildID, false)
		return
	}

	if err := player.Play(ctx, next.StreamURL, next.RequesterID); err != nil {
		s.logger.Error("playing next queued track", "guild_id", guildID, "error", err)
		queue.Clear()
		s.clearPlaybackClock(guildID)
		s.emitPlayback(guildID, false)
	} else {
		s.markPlaybackStart(guildID)
		s.emitPlayback(guildID, false)
	}
}

func (s *Service) syncPresence(session *discordgo.Session) {
	if session == nil {
		return
	}
	s.mu.Lock()
	guildIDs := make([]string, 0, len(s.queues))
	for id := range s.queues {
		guildIDs = append(guildIDs, id)
	}
	s.mu.Unlock()

	var label string
	for _, guildID := range guildIDs {
		state := s.PlaybackState(guildID)
		if state.Now == nil {
			continue
		}
		title := strings.TrimSpace(state.Now.Title)
		artist := strings.TrimSpace(state.Now.Artist)
		switch {
		case title != "" && artist != "":
			label = title + " — " + artist
		case title != "":
			label = title
		default:
			label = "music"
		}
		break
	}

	status := "online"
	activities := []*discordgo.Activity{}
	if label != "" {
		if len([]rune(label)) > 120 {
			r := []rune(label)
			label = string(r[:117]) + "…"
		}
		activities = []*discordgo.Activity{{
			Name: label,
			Type: discordgo.ActivityTypeListening,
		}}
	} else {
		activities = []*discordgo.Activity{{
			Name: "soft nights · /play",
			Type: discordgo.ActivityTypeListening,
		}}
	}
	_ = session.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status:     status,
		Activities: activities,
	})
}

const autoLeaveAfter = 60 * time.Second

// ConsiderAutoLeave starts/cancels a timer when the bot is alone in voice.
func (s *Service) ConsiderAutoLeave(session *discordgo.Session, guildID string) {
	if session == nil || guildID == "" || session.State == nil || session.State.User == nil {
		return
	}
	botID := session.State.User.ID
	channelID := BotVoiceChannel(session, guildID, botID)
	if channelID == "" {
		s.cancelAutoLeave(guildID)
		return
	}
	if humansInVoice(session, guildID, channelID, botID) > 0 {
		s.cancelAutoLeave(guildID)
		return
	}

	s.leaveMu.Lock()
	defer s.leaveMu.Unlock()
	if existing, ok := s.leaveTimers[guildID]; ok {
		existing.Stop()
	}
	s.leaveTimers[guildID] = time.AfterFunc(autoLeaveAfter, func() {
		s.leaveMu.Lock()
		delete(s.leaveTimers, guildID)
		s.leaveMu.Unlock()

		if BotVoiceChannel(session, guildID, botID) == "" {
			return
		}
		if humansInVoice(session, guildID, channelID, botID) > 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.Leave(ctx, session, botID, guildID); err != nil {
			s.logger.Warn("auto-leave failed", "guild_id", guildID, "error", err)
			return
		}
		s.logger.Info("auto-left empty voice channel", "guild_id", guildID)
	})
}

func (s *Service) cancelAutoLeave(guildID string) {
	s.leaveMu.Lock()
	defer s.leaveMu.Unlock()
	if t, ok := s.leaveTimers[guildID]; ok {
		t.Stop()
		delete(s.leaveTimers, guildID)
	}
}

func humansInVoice(session *discordgo.Session, guildID, channelID, botUserID string) int {
	guild, err := session.State.Guild(guildID)
	if err != nil || guild == nil {
		return 0
	}
	count := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID != channelID || vs.UserID == botUserID {
			continue
		}
		member, err := session.State.Member(guildID, vs.UserID)
		if err == nil && member != nil && member.User != nil && member.User.Bot {
			continue
		}
		count++
	}
	return count
}
