package music

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxSongFactLen = 380

var songFactHTTP = &http.Client{Timeout: 3 * time.Second}

// SongFact returns a short trivia line about the playing track.
func SongFact(ctx context.Context, artist, title string, durationSec int, requester string) string {
	for _, query := range songFactQueries(artist, title) {
		if fact := fetchWikipediaFact(ctx, query); fact != "" {
			return fact
		}
	}
	return fallbackSongFact(artist, title, durationSec, requester)
}

func songFactQueries(artist, title string) []string {
	artist = strings.TrimSpace(artist)
	title = CleanDisplayTitle(title)

	var queries []string
	if artist != "" && title != "" {
		queries = append(queries, artist+" "+title, title+" "+artist+" song")
	}
	if title != "" {
		queries = append(queries, title+" song")
	}
	if artist != "" {
		queries = append(queries, artist+" band", artist)
	}
	return queries
}

func fetchWikipediaFact(ctx context.Context, search string) string {
	title := wikipediaSearchTitle(ctx, search)
	if title == "" {
		return ""
	}
	if fact := fetchWikipediaSummary(ctx, title); fact != "" {
		return fact
	}
	return wikipediaSearchDescription(ctx, search)
}

func wikipediaSearchTitle(ctx context.Context, search string) string {
	endpoint := "https://en.wikipedia.org/w/api.php?action=opensearch&search=" +
		url.QueryEscape(search) + "&limit=1&namespace=0&format=json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}

	resp, err := songFactHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var payload []any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload) < 2 {
		return ""
	}

	titles, ok := payload[1].([]any)
	if !ok || len(titles) == 0 {
		return ""
	}
	title, ok := titles[0].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(title)
}

func wikipediaSearchDescription(ctx context.Context, search string) string {
	endpoint := "https://en.wikipedia.org/w/api.php?action=opensearch&search=" +
		url.QueryEscape(search) + "&limit=1&namespace=0&format=json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}

	resp, err := songFactHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var payload []any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload) < 3 {
		return ""
	}

	descriptions, ok := payload[2].([]any)
	if !ok || len(descriptions) == 0 {
		return ""
	}
	desc, ok := descriptions[0].(string)
	if !ok {
		return ""
	}
	return trimSongFact(desc)
}

func fetchWikipediaSummary(ctx context.Context, title string) string {
	endpoint := "https://en.wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(title)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}

	resp, err := songFactHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var payload struct {
		Extract string `json:"extract"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	return trimSongFact(payload.Extract)
}

func trimSongFact(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxSongFactLen {
		return text
	}
	return string(runes[:maxSongFactLen-1]) + "…"
}

func fallbackSongFact(artist, title string, durationSec int, requester string) string {
	title = CleanDisplayTitle(title)
	artist = strings.TrimSpace(artist)

	var options []string
	if artist != "" && title != "" {
		options = append(options, fmt.Sprintf("You're listening to **%s** by **%s**.", title, artist))
	} else if title != "" {
		options = append(options, fmt.Sprintf("You're listening to **%s**.", title))
	}
	if durationSec > 0 {
		if artist != "" && title != "" {
			options = append(options, fmt.Sprintf("**%s** runs for %s.", title, FormatDuration(durationSec)))
		} else {
			options = append(options, fmt.Sprintf("This track is %s long.", FormatDuration(durationSec)))
		}
	}
	if requester != "" {
		options = append(options, fmt.Sprintf("%s queued this one.", requester))
	}
	if len(options) == 0 {
		return "Couldn't find trivia for this track — but it still slaps."
	}
	return options[rand.IntN(len(options))]
}
