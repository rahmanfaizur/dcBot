package music

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

// Live network check for the yt-dlp -> ffmpeg pipe. Opt in with MUSIC_LIVE=1.
func TestOpenAudioStreamLive(t *testing.T) {
	if os.Getenv("MUSIC_LIVE") == "" {
		t.Skip("set MUSIC_LIVE=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	y := YTDLP{Binary: "yt-dlp"}
	stream, err := y.OpenAudioStream(ctx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("OpenAudioStream: %v", err)
	}
	defer stream.Close()

	buf := make([]byte, 256*1024)
	n, err := io.ReadFull(stream, buf)
	if n == 0 {
		t.Fatalf("no audio bytes read: %v", err)
	}
	t.Logf("read %d bytes of audio from the pipe", n)
}

// End-to-end check that the proxy serves decodable MP3 from the pipe.
func TestStreamProxyLive(t *testing.T) {
	if os.Getenv("MUSIC_LIVE") == "" {
		t.Skip("set MUSIC_LIVE=1 to run")
	}

	proxy, err := NewStreamProxy(slog.Default(), YTDLP{Binary: "yt-dlp"}, "ffmpeg")
	if err != nil {
		t.Fatalf("NewStreamProxy: %v", err)
	}
	defer proxy.Close()

	url := proxy.RegisterSource("https://www.youtube.com/watch?v=dQw4w9WgXcQ")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}

	head := make([]byte, 4096)
	n, err := io.ReadFull(resp.Body, head)
	if n < len(head) {
		t.Fatalf("only %d bytes of mp3: %v", n, err)
	}
	// MP3 frames start with 0xFF 0xEx, or the stream opens with an ID3 tag.
	if !bytes.HasPrefix(head, []byte("ID3")) && !(head[0] == 0xFF && head[1]&0xE0 == 0xE0) {
		t.Fatalf("output does not look like mp3: % x", head[:8])
	}
	t.Logf("proxy served %d bytes of valid mp3", n)
}
