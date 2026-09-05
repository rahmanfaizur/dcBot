package music

import (
	"math/rand/v2"
	"sync"
)

// LoopMode controls what happens when a track ends.
type LoopMode int

const (
	LoopOff   LoopMode = iota
	LoopTrack          // repeat the current song
	LoopQueue          // re-queue finished tracks
)

// QueueItem is one track waiting to play in a guild.
type QueueItem struct {
	Title         string
	Artist        string
	Thumbnail     string
	PageURL       string
	DurationSec   int
	StreamURL     string
	RequesterID   string
	RequesterName string
}

// Queue stores guild playback items in FIFO order.
type Queue struct {
	mu     sync.Mutex
	items  []QueueItem
	now    *QueueItem
	active bool
	loop   LoopMode
}

// NewQueue creates an empty queue.
func NewQueue() *Queue {
	return &Queue{}
}

// Enqueue appends a track to the queue.
func (q *Queue) Enqueue(item QueueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
}

// SetNowPlaying marks the item currently playing.
func (q *Queue) SetNowPlaying(item QueueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	copy := item
	q.now = &copy
	q.active = true
}

// Advance removes the finished track and returns the next one, if any.
func (q *Queue) Advance() (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.now = nil
	if len(q.items) == 0 {
		q.active = false
		return QueueItem{}, false
	}

	next := q.items[0]
	q.items = q.items[1:]
	copy := next
	q.now = &copy
	return next, true
}

// Skip drops the current track and returns the next queued item.
func (q *Queue) Skip() (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.now = nil
	if len(q.items) == 0 {
		q.active = false
		return QueueItem{}, false
	}

	next := q.items[0]
	q.items = q.items[1:]
	copy := next
	q.now = &copy
	return next, true
}

// Clear removes all queued tracks and the now-playing marker.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
	q.now = nil
	q.active = false
}

// Snapshot returns a copy of the queue state for display.
func (q *Queue) Snapshot() (now *QueueItem, upcoming []QueueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.now != nil {
		copy := *q.now
		now = &copy
	}
	upcoming = append([]QueueItem(nil), q.items...)
	return now, upcoming
}

// IsActive reports whether the queue is driving playback.
func (q *Queue) IsActive() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.active
}

// SetActive marks whether the queue should continue after track end.
func (q *Queue) SetActive(active bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.active = active
}

// Len returns the number of upcoming tracks, excluding now playing.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// RemoveUpcoming drops a 1-based upcoming track index.
func (q *Queue) RemoveUpcoming(index int) (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 1 || index > len(q.items) {
		return QueueItem{}, false
	}
	removed := q.items[index-1]
	q.items = append(q.items[:index-1], q.items[index:]...)
	return removed, true
}

// MoveUpcomingToFront moves a 1-based upcoming track to play next.
func (q *Queue) MoveUpcomingToFront(index int) (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 1 || index > len(q.items) {
		return QueueItem{}, false
	}
	item := q.items[index-1]
	q.items = append(q.items[:index-1], q.items[index:]...)
	q.items = append([]QueueItem{item}, q.items...)
	return item, true
}

// Loop returns the current loop mode.
func (q *Queue) Loop() LoopMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.loop
}

// SetLoop sets the loop mode.
func (q *Queue) SetLoop(mode LoopMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.loop = mode
}

// ShuffleUpcoming randomly reorders upcoming tracks. Returns false if fewer than 2.
func (q *Queue) ShuffleUpcoming() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) < 2 {
		return false
	}
	rand.Shuffle(len(q.items), func(i, j int) {
		q.items[i], q.items[j] = q.items[j], q.items[i]
	})
	return true
}

// NowCopy returns a copy of the current track, if any.
func (q *Queue) NowCopy() (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.now == nil {
		return QueueItem{}, false
	}
	return *q.now, true
}
