package music

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ytdlpCacheDir = "/tmp/yt-dlp-cache"

var youtubeMetadataClients = []string{
	"android,tv_embedded,web",
	"tv_embedded",
	"android",
}

var youtubeStreamClients = []string{
	"android",
	"tv_embedded",
	"android,tv_embedded",
	"web",
}

var youtubeStreamFormats = []string{
	"ba/b/w",
	"bestaudio/best/b/worst",
	"b/w",
}

// YTDLP holds yt-dlp binary settings shared by the resolver and stream proxy.
type YTDLP struct {
	Binary      string
	CookiesFile string
	Proxy       string
}

func (y YTDLP) binary() string {
	if strings.TrimSpace(y.Binary) == "" {
		return "yt-dlp"
	}
	return y.Binary
}

// PrepareCookiesFile copies a read-only cookies file into a writable cache path.
// yt-dlp updates cookies on exit; Docker mounts secrets read-only.
func PrepareCookiesFile(readOnlyPath string) (string, error) {
	readOnlyPath = strings.TrimSpace(readOnlyPath)
	if readOnlyPath == "" {
		return "", nil
	}
	data, err := os.ReadFile(readOnlyPath)
	if err != nil {
		return "", fmt.Errorf("reading cookies file: %w", err)
	}
	if err := os.MkdirAll(ytdlpCacheDir, 0o1777); err != nil {
		return "", fmt.Errorf("creating yt-dlp cache dir: %w", err)
	}
	dst := filepath.Join(ytdlpCacheDir, "cookies.txt")
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", fmt.Errorf("writing cookies cache: %w", err)
	}
	return dst, nil
}

func (y YTDLP) baseArgs() []string {
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"--no-progress",
		"--cache-dir", ytdlpCacheDir,
		"--js-runtimes", "node:/usr/bin/node",
	}
	if proxy := strings.TrimSpace(y.Proxy); proxy != "" {
		args = append(args, "--proxy", proxy)
	}
	return args
}

func (y YTDLP) commonArgs() []string {
	return y.argsForYouTubeClients(youtubeMetadataClients[0])
}

func (y YTDLP) argsForYouTubeClients(clients string) []string {
	args := append(y.baseArgs(), "--extractor-args", "youtube:player_client="+clients)
	if y.CookiesFile != "" {
		if _, err := os.Stat(y.CookiesFile); err == nil {
			args = append(args, "--cookies", y.CookiesFile)
		}
	}
	return args
}

// ResolveStreamURL finds a direct media URL for ffmpeg using several YouTube clients.
// mweb is intentionally excluded — it often returns storyboard-only formats on cloud IPs.
func (y YTDLP) ResolveStreamURL(ctx context.Context, target string) (string, error) {
	var lastErr error
	for _, clients := range youtubeStreamClients {
		for _, format := range youtubeStreamFormats {
			args := append(y.argsForYouTubeClients(clients),
				"--socket-timeout", "30",
				"-f", format,
				"--get-url",
				target,
			)
			cmd := exec.CommandContext(ctx, y.binary(), args...)
			output, err := captureYTDLPOutput(cmd)
			if err != nil {
				lastErr = err
				continue
			}
			if mediaURL := firstHTTPURL(output); mediaURL != "" {
				return mediaURL, nil
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("yt-dlp returned no stream URL")
	}
	return "", fmt.Errorf("yt-dlp stream URL lookup failed: %w", lastErr)
}

func firstHTTPURL(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line
		}
	}
	return ""
}

func captureYTDLPOutput(cmd *exec.Cmd) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var outBuf, errBuf bytes.Buffer
	outDone := make(chan error, 1)
	errDone := make(chan error, 1)
	go func() { _, err := io.Copy(&outBuf, stdout); outDone <- err }()
	go func() { _, err := io.Copy(&errBuf, stderr); errDone <- err }()

	if err := <-outDone; err != nil {
		_ = cmd.Wait()
		return "", err
	}
	if err := <-errDone; err != nil {
		_ = cmd.Wait()
		return "", err
	}

	waitErr := cmd.Wait()
	stdoutText := strings.TrimSpace(outBuf.String())
	stderrText := strings.TrimSpace(errBuf.String())

	if waitErr != nil {
		if stderrText != "" {
			return stdoutText, &ytdlpExecError{err: waitErr, stderr: stderrText}
		}
		return stdoutText, waitErr
	}
	return stdoutText, nil
}

type ytdlpExecError struct {
	err    error
	stderr string
}

func (e *ytdlpExecError) Error() string {
	if e.stderr != "" {
		return e.err.Error() + ": " + e.stderr
	}
	return e.err.Error()
}

func (e *ytdlpExecError) Unwrap() error {
	return e.err
}

type ytdlpMetadata struct {
	Title       string          `json:"title"`
	FullTitle   string          `json:"fulltitle"`
	Thumbnail   string          `json:"thumbnail"`
	WebpageURL  string          `json:"webpage_url"`
	OriginalURL string          `json:"original_url"`
	Duration    float64         `json:"duration"`
	ID          string          `json:"id"`
	DisplayID   string          `json:"display_id"`
	Entries     []ytdlpMetadata `json:"entries"`
}

func parseYTDLPMetadataJSON(output string) (title, thumbnail, pageURL string, durationSec int, err error) {
	var root ytdlpMetadata
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &root); err != nil {
		return "", "", "", 0, err
	}

	meta := root
	if len(root.Entries) > 0 {
		entry := root.Entries[0]
		if entry.Title == "" && entry.FullTitle == "" && entry.DisplayID == "" && entry.ID == "" {
			return "", "", "", 0, fmt.Errorf("yt-dlp returned an empty search result")
		}
		meta = entry
	}

	title = CleanDisplayTitle(meta.Title)
	if title == "" {
		title = CleanDisplayTitle(meta.FullTitle)
	}
	if title == "" {
		return "", "", "", 0, fmt.Errorf("yt-dlp did not return a title")
	}

	pageURL = youtubeWatchURL(meta)
	if meta.Duration > 0 {
		durationSec = int(meta.Duration + 0.5)
	}
	return title, meta.Thumbnail, pageURL, durationSec, nil
}

func youtubeWatchURL(meta ytdlpMetadata) string {
	for _, candidate := range []string{meta.WebpageURL, meta.OriginalURL} {
		if isYouTubeWatchURL(candidate) {
			return candidate
		}
	}
	id := meta.ID
	if id == "" {
		id = meta.DisplayID
	}
	if id != "" && !strings.Contains(id, " ") {
		return "https://www.youtube.com/watch?v=" + id
	}
	return ""
}

func isYouTubeWatchURL(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	return strings.Contains(lower, "youtube.com/watch") || strings.Contains(lower, "youtu.be/")
}

func streamTargetForPlayback(target, pageURL string) string {
	if isYouTubeWatchURL(pageURL) {
		return pageURL
	}
	return target
}
