package music

import (
	"context"
	"fmt"

	"github.com/faizur/mybot/internal/linkdave"
)

// EnqueueResult describes what Enqueue added to the guild player.
type EnqueueResult struct {
	Tracks        []ResolvedTrack
	Started       bool
	Playlist      bool
	PlaylistTotal int
}

// Enqueue resolves input and either starts playback or appends to the queue.
// Call EnsureVoice before this when the bot may need to join a channel.
func (s *Service) Enqueue(ctx context.Context, guildID, query, requesterID, requesterName string) (EnqueueResult, error) {
	player := s.linkdave.Player(guildID)
	s.wireGuild(guildID)

	if !player.Connected() {
		return EnqueueResult{}, fmt.Errorf("bot is not connected to voice — try /join first")
	}

	s.resetStaleQueue(guildID)

	if IsPlaylistURL(query) {
		return s.enqueuePlaylist(ctx, guildID, query, requesterID, requesterName)
	}

	track, err := s.resolver.Resolve(ctx, query)
	if err != nil {
		s.logger.Warn("resolve failed", "query", query, "error", err)
		return EnqueueResult{}, err
	}

	started, err := s.enqueueItems(ctx, guildID, []QueueItem{trackToQueueItem(track, requesterID, requesterName)})
	if err != nil {
		return EnqueueResult{}, err
	}
	return EnqueueResult{Tracks: []ResolvedTrack{track}, Started: started}, nil
}

func (s *Service) enqueuePlaylist(ctx context.Context, guildID, query, requesterID, requesterName string) (EnqueueResult, error) {
	tracks, err := s.resolver.ResolvePlaylist(ctx, query, MaxPlaylistTracks)
	if err != nil {
		s.logger.Warn("playlist resolve failed", "query", query, "error", err)
		return EnqueueResult{}, err
	}

	items := make([]QueueItem, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, trackToQueueItem(track, requesterID, requesterName))
	}

	started, err := s.enqueueItems(ctx, guildID, items)
	if err != nil {
		return EnqueueResult{}, err
	}

	return EnqueueResult{
		Tracks:        tracks,
		Started:       started,
		Playlist:      true,
		PlaylistTotal: len(tracks),
	}, nil
}

func (s *Service) enqueueItems(ctx context.Context, guildID string, items []QueueItem) (bool, error) {
	if len(items) == 0 {
		return false, fmt.Errorf("nothing to enqueue")
	}

	player := s.linkdave.Player(guildID)
	queue := s.queue(guildID)

	if player.State() == linkdave.PlayerStatePlaying || player.State() == linkdave.PlayerStatePaused || queue.IsActive() {
		for _, item := range items {
			queue.Enqueue(item)
		}
		s.emitPlayback(guildID, true)
		return false, nil
	}

	first := items[0]
	queue.SetActive(true)
	queue.SetNowPlaying(first)
	if err := player.Play(ctx, first.StreamURL, first.RequesterID); err != nil {
		queue.Clear()
		s.clearPlaybackClock(guildID)
		return false, err
	}
	for _, item := range items[1:] {
		queue.Enqueue(item)
	}
	s.markPlaybackStart(guildID)
	s.emitPlayback(guildID, false)
	return true, nil
}

func trackToQueueItem(track ResolvedTrack, requesterID, requesterName string) QueueItem {
	return QueueItem{
		Title:         track.Title,
		Artist:        track.Artist,
		Thumbnail:     track.Thumbnail,
		PageURL:       track.PageURL,
		DurationSec:   track.DurationSec,
		StreamURL:     track.StreamURL,
		RequesterID:   requesterID,
		RequesterName: requesterName,
	}
}

// RemoveQueueItem removes a 1-based upcoming queue entry.
func (s *Service) RemoveQueueItem(guildID string, index int) (QueueItem, bool) {
	removed, ok := s.queue(guildID).RemoveUpcoming(index)
	if ok {
		s.emitPlayback(guildID, true)
	}
	return removed, ok
}

// MoveQueueToFront moves a 1-based upcoming queue entry to play next.
func (s *Service) MoveQueueToFront(guildID string, index int) (QueueItem, bool) {
	moved, ok := s.queue(guildID).MoveUpcomingToFront(index)
	if ok {
		s.emitPlayback(guildID, true)
	}
	return moved, ok
}
