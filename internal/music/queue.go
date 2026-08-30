package music

import (
	"sync"
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
