// Command bot is the Discord bot entrypoint.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/faizur/mybot/internal/bot"
	"github.com/faizur/mybot/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("loading config", "error", err)
		os.Exit(1)
	}

	if level, parseErr := parseLogLevel(cfg.LogLevel); parseErr == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
		slog.SetDefault(logger)
	}

	b, err := bot.New(cfg, logger)
	if err != nil {
		logger.Error("creating bot", "error", err)
		os.Exit(1)
	}

	ctx, cancel := bot.WaitForSignal(context.Background())
	defer cancel()

	if err := b.Run(ctx); err != nil {
		logger.Error("bot exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}

func parseLogLevel(level string) (slog.Level, error) {
	var l slog.Level
	err := l.UnmarshalText([]byte(level))
	return l, err
}
