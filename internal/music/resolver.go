package music

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const searchPrefix = "search:"

type inputKind int

const (
	inputSearch inputKind = iota
	inputDirect
	inputYouTube
	inputSoundCloud
	inputSpotify
)

var spotifyHostPattern = regexp.MustCompile(`(?i)^https?://(?:open\.)?spotify\.com/`)

// ResolvedTrack is a playable audio source for Linkdave.
type ResolvedTrack struct {
	Title       string
	Artist      string
	Thumbnail   string
	PageURL     string
	DurationSec int
	StreamURL   string
}

// searchCacheTTL keeps autocomplete snappy: Discord fires a request per
// keystroke, so repeated prefixes should not re-run yt-dlp.
const searchCacheTTL = 5 * time.Minute

// Resolver turns user queries and links into playable stream URLs.
type Resolver struct {
	ytdlp      YTDLP
	proxy      *StreamProxy
	httpClient *http.Client

	searchMu    sync.Mutex
	searchCache map[string]searchCacheEntry
}

type searchCacheEntry struct {
	results   []SearchResult
	expiresAt time.Time
}

// NewResolver creates a track resolver backed by yt-dlp for search and platform links.
func NewResolver(ytdlp YTDLP, proxy *StreamProxy) *Resolver {
	return &Resolver{
		ytdlp: ytdlp,
		proxy: proxy,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
		searchCache: make(map[string]searchCacheEntry),
	}
}

// SearchResult is a lightweight track match for autocomplete.
type SearchResult struct {
	ID          string
	Title       string
	Uploader    string
	DurationSec int
}

// Search returns real YouTube video matches for slash-command autocomplete.
// YouTube search still works from cloud hosts even when audio playback there
// does not, so suggestions stay accurate regardless of where the bot runs.
func (r *Resolver) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 25 {
		limit = 25
	}

	key := strings.ToLower(query)
	if cached, ok := r.cachedSearch(key, limit); ok {
		return cached, nil
	}

	results, err := r.ytdlp.SearchTracks(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	r.storeSearch(key, results)
	return results, nil
}

func (r *Resolver) cachedSearch(key string, limit int) ([]SearchResult, bool) {
	r.searchMu.Lock()
	defer r.searchMu.Unlock()

	entry, ok := r.searchCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	if len(entry.results) > limit {
		return entry.results[:limit], true
	}
	return entry.results, true
}

func (r *Resolver) storeSearch(key string, results []SearchResult) {
	r.searchMu.Lock()
	defer r.searchMu.Unlock()

	if r.searchCache == nil || len(r.searchCache) > 256 {
		r.searchCache = make(map[string]searchCacheEntry, 32)
	}
	r.searchCache[key] = searchCacheEntry{
		results:   results,
		expiresAt: time.Now().Add(searchCacheTTL),
	}
}

// Resolve converts a user query or link into a stream URL Linkdave can play.
func (r *Resolver) Resolve(ctx context.Context, input string) (ResolvedTrack, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return ResolvedTrack{}, fmt.Errorf("query is required")
	}

	if classifyInput(input) == inputSpotify {
		meta, err := r.spotifyMetadata(ctx, input)
		if err != nil {
			return ResolvedTrack{}, err
		}
		if r.proxy == nil {
			return ResolvedTrack{}, fmt.Errorf("music stream proxy is not configured")
		}
		artist, songTitle := splitTrackTitle(meta.Title)
		if songTitle == "" {
			songTitle = CleanDisplayTitle(meta.Title)
		}
		return ResolvedTrack{
			Title:     songTitle,
			Artist:    artist,
			Thumbnail: meta.Thumbnail,
			PageURL:   input,
			StreamURL: r.proxy.RegisterSource("ytsearch1:" + meta.Title),
		}, nil
	}

	target := normalizeResolveTarget(input)

	if classifyInput(input) == inputDirect {
		title := directTitle(input)
		return ResolvedTrack{Title: title, PageURL: input, StreamURL: input}, nil
	}

	title, thumbnail, pageURL, durationSec, err := r.resolveMetadata(ctx, target)
	if err != nil {
		return ResolvedTrack{}, err
	}
	if r.proxy == nil {
		return ResolvedTrack{}, fmt.Errorf("music stream proxy is not configured")
	}

	artist, songTitle := splitTrackTitle(title)
	if songTitle == "" {
		songTitle = CleanDisplayTitle(title)
	}

	return ResolvedTrack{
		Title:       songTitle,
		Artist:      artist,
		Thumbnail:   thumbnail,
		PageURL:     pageURL,
		DurationSec: durationSec,
		StreamURL:   r.proxy.RegisterSource(streamTargetForPlayback(target, pageURL)),
	}, nil
}

func normalizeResolveTarget(input string) string {
	if id, ok := strings.CutPrefix(input, "yt:"); ok {
		return "https://www.youtube.com/watch?v=" + id
	}
	if title, ok := strings.CutPrefix(input, searchPrefix); ok {
		return "ytsearch1:" + title
	}
	switch classifyInput(input) {
	case inputSpotify:
		return input
	case inputSearch:
		return "ytsearch1:" + input
	default:
		return input
	}
}

func directTitle(input string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(input), "/")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx < len(trimmed)-1 {
		return trimmed[idx+1:]
	}
	return trimmed
}

func classifyInput(input string) inputKind {
	lower := strings.ToLower(input)
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		switch {
		case strings.Contains(lower, "youtube.com/") || strings.Contains(lower, "youtu.be/"):
			return inputYouTube
		case strings.Contains(lower, "soundcloud.com/"):
			return inputSoundCloud
		case spotifyHostPattern.MatchString(lower):
			return inputSpotify
		default:
			return inputDirect
		}
	default:
		return inputSearch
	}
}

type spotifyMetadataResult struct {
	Title     string
	Thumbnail string
}

func (r *Resolver) spotifyMetadata(ctx context.Context, spotifyURL string) (spotifyMetadataResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://open.spotify.com/oembed?url="+url.QueryEscape(spotifyURL), nil)
	if err != nil {
		return spotifyMetadataResult{}, fmt.Errorf("building spotify oembed request: %w", err)
	}

	res, err := r.httpClient.Do(req)
	if err != nil {
		return spotifyMetadataResult{}, fmt.Errorf("fetching spotify metadata: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return spotifyMetadataResult{}, fmt.Errorf("fetching spotify metadata: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Title        string `json:"title"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return spotifyMetadataResult{}, fmt.Errorf("decoding spotify metadata: %w", err)
	}
	if strings.TrimSpace(payload.Title) == "" {
		return spotifyMetadataResult{}, fmt.Errorf("spotify track metadata did not include a title")
	}

	return spotifyMetadataResult{
		Title:     payload.Title,
		Thumbnail: payload.ThumbnailURL,
	}, nil
}

func (r *Resolver) resolveMetadata(ctx context.Context, target string) (title, thumbnail, pageURL string, durationSec int, err error) {
	var lastErr error
	for _, clients := range youtubeMetadataClients {
		args := append(r.ytdlp.argsForYouTubeClients(clients),
			"--socket-timeout", "30",
			"--no-download",
			"--ignore-no-formats-error",
			"--dump-single-json",
			target,
		)
		cmd := exec.CommandContext(ctx, r.ytdlp.binary(), args...)

		output, err := captureYTDLPOutput(cmd)
		if err != nil {
			lastErr = fmt.Errorf("resolving audio with yt-dlp: %w: %s", err, strings.TrimSpace(output))
			continue
		}

		title, thumbnail, pageURL, durationSec, err = parseYTDLPMetadataJSON(output)
		if err != nil {
			lastErr = fmt.Errorf("parsing yt-dlp metadata: %w", err)
			continue
		}
		return title, thumbnail, pageURL, durationSec, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("yt-dlp metadata lookup failed")
	}
	return "", "", "", 0, lastErr
}
