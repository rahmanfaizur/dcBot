package music

import (
	"errors"
	"strings"
)

// PlaybackError is user-facing copy for failed playback attempts.
type PlaybackError struct {
	Title       string
	Description string
}

// DescribePlaybackError returns structured UI text for common playback failures.
func DescribePlaybackError(err error) PlaybackError {
	if err == nil {
		return PlaybackError{}
	}

	info := PlaybackError{
		Title:       "Track unavailable",
		Description: "I couldn't load that one — it may be region-locked or removed. Try another link or search again with `/play`.",
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "timed out waiting for linkdave"):
		info.Title = "Voice connection failed"
		info.Description = "Voice connection timed out — run `/leave` then `/play` again (the bot may have restarted)."
	case strings.Contains(lower, "not connected to voice"):
		info.Title = "Not in voice"
		info.Description = "I'm not in a voice channel — run `/join` or `/play` from a voice channel first."
	case strings.Contains(lower, "yt-dlp"):
		info.Title = "Could not fetch audio"
		info.Description = "I couldn't download that track — update yt-dlp (`sudo snap install yt-dlp`) and try again."
	case strings.Contains(lower, "client.timeout exceeded"):
		info.Title = "Request timed out"
		info.Description = "The audio server took too long — try again or pick a shorter track."
	case strings.Contains(lower, "empty mp3 stream"):
		info.Title = "Playback failed"
		info.Description = "Audio streaming failed — restart Linkdave: `docker compose up -d linkdave --force-recreate`."
	case strings.Contains(lower, "not a valid url"), strings.Contains(lower, "could not find"):
		info.Title = "Track unavailable"
		info.Description = "I couldn't find that track — pick a result from the `/play` dropdown or paste a YouTube link."
	case strings.Contains(lower, "region"):
		info.Title = "Track unavailable"
		info.Description = "That track may be region-locked — try another link or search again with `/play`."
	}
	return info
}

// FriendlyControlError returns a safe message for transport button and slash control failures.
func FriendlyControlError(err error) string {
	if err == nil {
		return ""
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "player not found"):
		return "Playback session expired — try `/play` again."
	case strings.Contains(lower, "linkdave is not connected"):
		return "Voice isn't ready yet — run `/join` or `/play` first."
	case strings.Contains(lower, "not connected to voice"):
		return "I'm not in a voice channel — join one and try again."
	case strings.Contains(lower, "nothing playing"), strings.Contains(lower, "not playing"):
		return "Nothing is playing right now."
	case strings.Contains(lower, "already paused"):
		return "Playback is already paused."
	case strings.Contains(lower, "not paused"):
		return "Playback isn't paused."
	case strings.Contains(lower, "timed out waiting for linkdave"):
		return "Voice connection timed out — run `/leave` then `/play` again."
	}

	if msg := FriendlyError(err); isSafeUserMessage(msg) {
		return msg
	}
	return "Something went wrong — try `/play` again."
}

func isSafeUserMessage(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "linkdave"),
		strings.Contains(lower, "post /"),
		strings.Contains(lower, "http://"),
		strings.Contains(lower, "https://"),
		strings.Contains(lower, "{"),
		strings.Contains(lower, "sessions/"),
		strings.Contains(lower, "players/"):
		return false
	}
	return len(msg) <= 180
}

// FriendlyError returns a short user-facing message for common playback failures.
func FriendlyError(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "timed out waiting for linkdave"):
		return "voice connection timed out — run /leave then /play again (bot may have restarted)"
	case strings.Contains(lower, "yt-dlp"):
		return "could not fetch audio — update yt-dlp: `sudo snap install yt-dlp` then retry"
	case strings.Contains(lower, "client.timeout exceeded"):
		return "audio server took too long — try again or pick a shorter track"
	case strings.Contains(lower, "empty mp3 stream"):
		return "playback failed — restart linkdave: docker compose up -d linkdave --force-recreate"
	case strings.Contains(lower, "not a valid url"):
		return "could not find that track — try picking from the dropdown or paste a YouTube link"
	case strings.Contains(lower, "player not found"):
		return "playback session expired — try /play again"
	case strings.Contains(lower, "linkdave"):
		return "voice playback is unavailable — try /play again"
	default:
		return "something went wrong — try /play again"
	}
}

// WrapResolveError annotates resolver failures for logging while keeping UI text short.
func WrapResolveError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(FriendlyError(err))
}
