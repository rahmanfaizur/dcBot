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

// Resolver turns user queries and links into playable stream URLs.
type Resolver struct {
	ytdlpPath  string
	proxy      *StreamProxy
	httpClient *http.Client
}

// NewResolver creates a track resolver backed by yt-dlp for search and platform links.
func NewResolver(ytdlpPath string, proxy *StreamProxy) *Resolver {
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	return &Resolver{
		ytdlpPath: ytdlpPath,
		proxy:     proxy,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// SearchResult is a lightweight track match for autocomplete.
type SearchResult struct {
	ID    string
	Title string
}

// Search returns fast YouTube title suggestions for slash-command autocomplete.
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

	endpoint := "https://suggestqueries.google.com/complete/search?client=firefox&ds=yt&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building youtube suggest request: %w", err)
	}

	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching youtube suggestions: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 65536))
	if err != nil {
		return nil, fmt.Errorf("reading youtube suggestions: %w", err)
	}

	var payload []json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || len(payload) < 2 {
		return nil, fmt.Errorf("parsing youtube suggestions")
	}

	var suggestions []string
	if err := json.Unmarshal(payload[1], &suggestions); err != nil {
		return nil, fmt.Errorf("decoding youtube suggestions: %w", err)
	}

	results := make([]SearchResult, 0, min(limit, len(suggestions)))
	seen := make(map[string]struct{}, len(suggestions))
	for _, title := range suggestions {
		title = strings.TrimSpace(title)
		if len(title) < 2 {
			continue
		}
		key := strings.ToLower(title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, SearchResult{Title: title})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
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
			Title:       songTitle,
			Artist:      artist,
			Thumbnail:   meta.Thumbnail,
			PageURL:     input,
			StreamURL:   r.proxy.RegisterSource("ytsearch1:" + meta.Title),
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
		StreamURL:   r.proxy.RegisterSource(target),
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
	args := append(ytdlpCommonArgs(),
		"--socket-timeout", "30",
		"--no-download",
		"--dump-single-json",
		target,
	)
	cmd := exec.CommandContext(ctx, r.ytdlpPath, args...)

	output, err := captureYTDLPOutput(cmd)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("resolving audio with yt-dlp: %w: %s", err, strings.TrimSpace(output))
	}

	var payload struct {
		Title      string  `json:"title"`
		Thumbnail  string  `json:"thumbnail"`
		WebpageURL string  `json:"webpage_url"`
		Duration   float64 `json:"duration"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload); err != nil {
		return "", "", "", 0, fmt.Errorf("parsing yt-dlp metadata: %w: %s", err, strings.TrimSpace(output))
	}

	title = CleanDisplayTitle(payload.Title)
	if title == "" {
		return "", "", "", 0, fmt.Errorf("yt-dlp did not return a title")
	}
	if payload.Duration > 0 {
		durationSec = int(payload.Duration + 0.5)
	}
	return title, payload.Thumbnail, payload.WebpageURL, durationSec, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
