package music

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	titleNoise = regexp.MustCompile(`(?i)\s*[\(\[]?\s*(official\s*(music\s*)?video|official\s*mv|official\s*audio|lyrics?(\s*video)?|audio(\s*only)?|mv|4k(\s*remaster)?)\s*[\)\]]?\s*$`)
)

// splitTrackTitle heuristically splits "Artist - Song" style titles.
func splitTrackTitle(full string) (artist, title string) {
	full = strings.TrimSpace(full)
	if full == "" {
		return "", ""
	}

	for _, sep := range []string{" - ", " – ", " — ", " | ", " · "} {
		if parts := strings.SplitN(full, sep, 2); len(parts) == 2 {
			a := strings.TrimSpace(parts[0])
			t := strings.TrimSpace(parts[1])
			t = titleNoise.ReplaceAllString(t, "")
			t = strings.TrimSpace(t)
			if a != "" && t != "" {
				return a, t
			}
		}
	}

	return "", CleanDisplayTitle(full)
}

func CleanDisplayTitle(title string) string {
	title = strings.TrimSpace(title)
	title = titleNoise.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func parseYTDLPPrint(output string) (title, thumbnail, pageURL string, durationSec int) {
	lines := sanitizeYTDLPOutput(output)
	if len(lines) == 0 {
		return "", "", "", 0
	}

	title = CleanDisplayTitle(lines[0])
	for _, line := range lines[1:] {
		switch {
		case strings.Contains(line, "ytimg.com") || strings.Contains(line, "ggpht.com"):
			thumbnail = line
		case strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://"):
			pageURL = line
		default:
			if durationSec == 0 {
				durationSec = parseDurationLine(line)
			}
		}
	}
	return title, thumbnail, pageURL, durationSec
}

func parseDurationLine(line string) int {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int(seconds + 0.5)
}

// FormatDuration renders seconds as m:ss or h:mm:ss.
func FormatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func sanitizeYTDLPOutput(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isYTDLPNoise(line) {
			continue
		}
		clean = append(clean, line)
	}
	return clean
}

func isYTDLPNoise(line string) bool {
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "warning"):
		return true
	case strings.HasPrefix(lower, "error:"):
		return true
	case strings.Contains(lower, "deprecated"):
		return true
	case strings.Contains(lower, "please update to python"):
		return true
	default:
		return false
	}
}

