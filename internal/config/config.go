// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all environment-driven settings for the bot process.
type Config struct {
	DiscordToken     string
	DiscordGuild     string
	LinkdaveURL      string
	LinkdavePass     string
	YTDLPPath        string
	FFMPEGPath       string
	YTDLPCookiesFile string
	YTDLPProxy       string
	MongoURI         string
	APIAddr          string
	LogLevel         string
}

// MusicEnabled reports whether Linkdave music settings are present.
func (c Config) MusicEnabled() bool {
	return c.LinkdaveURL != "" && c.LinkdavePass != ""
}

// Load reads configuration from the environment. A local .env file is loaded
// when present; missing .env is not an error so production can rely on real env vars.
func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		DiscordToken:     strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		DiscordGuild:     strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID")),
		LinkdaveURL:      strings.TrimSpace(os.Getenv("LINKDAVE_URL")),
		LinkdavePass:     strings.TrimSpace(os.Getenv("LINKDAVE_PASSWORD")),
		YTDLPPath:        resolveYTDLPPath(strings.TrimSpace(os.Getenv("YTDLP_PATH"))),
		FFMPEGPath:       resolveFFMPEGPath(strings.TrimSpace(os.Getenv("FFMPEG_PATH"))),
		YTDLPCookiesFile: strings.TrimSpace(os.Getenv("YTDLP_COOKIES_FILE")),
		YTDLPProxy:       strings.TrimSpace(os.Getenv("YTDLP_PROXY")),
		MongoURI:         strings.TrimSpace(os.Getenv("MONGODB_URI")),
		APIAddr:          strings.TrimSpace(os.Getenv("API_ADDR")),
		LogLevel:         strings.TrimSpace(os.Getenv("LOG_LEVEL")),
	}

	if cfg.DiscordToken == "" {
		return Config{}, fmt.Errorf("DISCORD_TOKEN is required")
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.APIAddr == "" {
		cfg.APIAddr = "127.0.0.1:8787"
	}

	return cfg, nil
}

func resolveYTDLPPath(configured string) string {
	if configured != "" && configured != "yt-dlp" {
		return configured
	}

	for _, candidate := range []string{"/snap/bin/yt-dlp", "/usr/local/bin/yt-dlp", "yt-dlp"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return "yt-dlp"
}

func resolveFFMPEGPath(configured string) string {
	if configured != "" && configured != "ffmpeg" {
		return configured
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}
	return "ffmpeg"
}
