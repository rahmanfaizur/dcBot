package music

import (
	"net/url"
	"strings"
)

const MaxPlaylistTracks = 50

// PlaylistEntry is lightweight metadata for one playlist item.
type PlaylistEntry struct {
	ID          string
	Title       string
	Artist      string
	DurationSec int
	PageURL     string
}

// IsPlaylistURL reports whether the input looks like a multi-track playlist link.
func IsPlaylistURL(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}

	lower := strings.ToLower(input)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}

	switch {
	case strings.Contains(lower, "youtube.com/") || strings.Contains(lower, "youtu.be/"):
		return strings.Contains(lower, "list=") && !isYouTubeMixRadio(lower)
	case spotifyHostPattern.MatchString(lower):
		return strings.Contains(lower, "/playlist/") || strings.Contains(lower, "/album/")
	default:
		return false
	}
}

// isYouTubeMixRadio filters RD* auto-radio lists that are not real user playlists.
func isYouTubeMixRadio(lower string) bool {
	if u, err := url.Parse(lower); err == nil {
		if list := u.Query().Get("list"); strings.HasPrefix(strings.ToUpper(list), "RD") {
			return true
		}
	}
	return strings.Contains(lower, "list=rd")
}
