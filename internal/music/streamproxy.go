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
	ytdlpPath  string
	ffmpegPath string
	baseURL    string
	server     *http.Server

	mu      sync.Mutex
	sources map[string]string
}

// NewStreamProxy starts a localhost HTTP server that transcodes yt-dlp output to MP3.
func NewStreamProxy(logger *slog.Logger, ytdlpPath, ffmpegPath string) (*StreamProxy, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	proxy := &StreamProxy{
		logger:     logger,
		ytdlpPath:  ytdlpPath,
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

	ytdlpArgs := append(ytdlpCommonArgs(),
		"--socket-timeout", "30",
		"-f", "bestaudio/best",
		"-o", "-",
		target,
	)
	ytdlp := exec.CommandContext(r.Context(), p.ytdlpPath, ytdlpArgs...)

	ytdlpStdout, err := ytdlp.StdoutPipe()
	if err != nil {
		p.logger.Error("creating yt-dlp stdout pipe", "error", err)
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}
	var ytdlpErr bytes.Buffer
	ytdlp.Stderr = &ytdlpErr

	ffmpeg := exec.CommandContext(r.Context(), p.ffmpegPath,
		"-nostdin",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-f", "mp3",
		"-ab", "128k",
		"pipe:1",
	)
	ffmpeg.Stdin = ytdlpStdout
	ffmpeg.Stderr = io.Discard

	if err := ytdlp.Start(); err != nil {
		p.logger.Error("starting yt-dlp stream", "error", err, "target", target)
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}

	ffmpegStdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		_ = ytdlp.Process.Kill()
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}

	if err := ffmpeg.Start(); err != nil {
		_ = ytdlp.Process.Kill()
		p.logger.Error("starting ffmpeg stream", "error", err)
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(w, ffmpegStdout); err != nil && r.Context().Err() == nil {
		p.logger.Warn("stream copy ended", "error", err, "target", target)
	}

	if err := ytdlp.Wait(); err != nil {
		stderr := strings.TrimSpace(ytdlpErr.String())
		if stderr != "" {
			p.logger.Warn("yt-dlp stream exited", "error", err, "stderr", stderr, "target", target)
		} else {
			p.logger.Warn("yt-dlp stream exited", "error", err, "target", target)
		}
	}
	_ = ffmpeg.Wait()
}

func randomStreamID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
