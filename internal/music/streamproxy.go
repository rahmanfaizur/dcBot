package music

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StreamProxy serves on-demand MP3 streams for Linkdave. YouTube and other
// sources return webm/m4a URLs from yt-dlp, but Linkdave only plays MP3 streams.
type StreamProxy struct {
	logger     *slog.Logger
	ytdlp      YTDLP
	ffmpegPath string
	baseURL    string
	server     *http.Server

	mu      sync.Mutex
	sources map[string]string
}

// NewStreamProxy starts a localhost HTTP server that transcodes yt-dlp output to MP3.
func NewStreamProxy(logger *slog.Logger, ytdlp YTDLP, ffmpegPath string) (*StreamProxy, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	proxy := &StreamProxy{
		logger:     logger,
		ytdlp:      ytdlp,
		ffmpegPath: ffmpegPath,
		sources:    make(map[string]string),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", proxy.handleStream)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting stream proxy listener: %w", err)
	}

	proxy.baseURL = "http://" + ln.Addr().String()
	proxy.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := proxy.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("stream proxy server stopped", "error", err)
		}
	}()

	logger.Info("stream proxy listening", "url", proxy.baseURL)
	return proxy, nil
}

// RegisterSource returns a localhost MP3 stream URL for the given yt-dlp target.
func (p *StreamProxy) RegisterSource(target string) string {
	id := randomStreamID()
	p.mu.Lock()
	p.sources[id] = target
	p.mu.Unlock()
	return p.baseURL + "/stream/" + id
}

// Close shuts down the stream proxy HTTP server.
func (p *StreamProxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.server.Shutdown(ctx)
}

func (p *StreamProxy) handleStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/stream/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	p.mu.Lock()
	target, ok := p.sources[id]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "audio/mpeg")

	audio, err := p.ytdlp.OpenAudioStream(r.Context(), target)
	if err != nil {
		p.logger.Warn("yt-dlp audio stream failed", "error", err, "target", target)
		http.Error(w, "stream setup failed", http.StatusBadGateway)
		return
	}
	defer audio.Close()

	ffmpeg := exec.CommandContext(r.Context(), p.ffmpegPath,
		"-loglevel", "error",
		"-i", "pipe:0",
		"-f", "mp3",
		"-ab", "128k",
		"pipe:1",
	)
	ffmpeg.Stdin = audio
	var ffmpegErr bytes.Buffer
	ffmpeg.Stderr = &ffmpegErr

	ffmpegStdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		p.logger.Error("creating ffmpeg stdout pipe", "error", err, "target", target)
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}

	if err := ffmpeg.Start(); err != nil {
		p.logger.Error("starting ffmpeg stream", "error", err, "target", target)
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(w, ffmpegStdout); err != nil && r.Context().Err() == nil {
		p.logger.Warn("stream copy ended", "error", err, "target", target)
	}

	if err := ffmpeg.Wait(); err != nil {
		stderr := strings.TrimSpace(ffmpegErr.String())
		if stderr != "" {
			p.logger.Warn("ffmpeg stream exited", "error", err, "stderr", stderr, "target", target)
		} else {
			p.logger.Warn("ffmpeg stream exited", "error", err, "target", target)
		}
	}
}

func randomStreamID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
