// Package bot wires discordgo, command registration, and lifecycle management.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/commands"
	"github.com/faizur/mybot/internal/config"
	"github.com/faizur/mybot/internal/linkdave"
	"github.com/faizur/mybot/internal/music"
)

// Bot owns the Discord session and command registry for the process.
type Bot struct {
	cfg         config.Config
	logger      *slog.Logger
	session     *discordgo.Session
	registry    *commands.Registry
	linkdave    *linkdave.Client
	streamProxy *music.StreamProxy
	music       *music.Service
}

// New creates a Bot from configuration and registers built-in commands.
func New(cfg config.Config, logger *slog.Logger) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("creating discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	registry := commands.NewRegistry(logger)
	registry.Register(commands.PingCommand())

	b := &Bot{
		cfg:      cfg,
		logger:   logger,
		session:  session,
		registry: registry,
	}

	if cfg.MusicEnabled() {
		ld, err := linkdave.NewClient(linkdave.Config{
			NodeURL:   cfg.LinkdaveURL,
			Password:  cfg.LinkdavePass,
			BotUserID: "0",
			Logger:    logger,
			UpdateVoice: func(guildID, channelID string, mute, deaf bool) error {
				return session.ChannelVoiceJoinManual(guildID, channelID, mute, deaf)
			},
		})
		if err != nil {
			return nil, fmt.Errorf("creating linkdave client: %w", err)
		}

		logger.Info("music enabled", "ytdlp", cfg.YTDLPPath, "ffmpeg", cfg.FFMPEGPath, "linkdave", cfg.LinkdaveURL)

		ytdlp := music.YTDLP{Binary: cfg.YTDLPPath}
		if cfg.YTDLPCookiesFile != "" {
			writableCookies, err := music.PrepareCookiesFile(cfg.YTDLPCookiesFile)
			if err != nil {
				logger.Warn("yt-dlp cookies unavailable", "path", cfg.YTDLPCookiesFile, "error", err)
			} else if writableCookies != "" {
				ytdlp.CookiesFile = writableCookies
				logger.Info("yt-dlp cookies enabled", "path", cfg.YTDLPCookiesFile)
			}
		}

		proxy, err := music.NewStreamProxy(logger, ytdlp, cfg.FFMPEGPath)
		if err != nil {
			return nil, fmt.Errorf("creating stream proxy: %w", err)
		}
		b.streamProxy = proxy
		b.linkdave = ld
		b.music = music.NewService(logger, ld, music.NewResolver(ytdlp, proxy))
		panel := commands.NewPlayerPanel(logger, b.music)
		registry.SetComponentHandler(panel.HandleComponent)
		for _, cmd := range commands.MusicCommands(b.music, panel) {
			registry.Register(cmd)
		}
	} else {
		logger.Warn("music disabled: set LINKDAVE_URL and LINKDAVE_PASSWORD to enable voice commands")
	}

	session.AddHandler(b.onInteractionCreate)
	session.AddHandler(b.onVoiceStateUpdate)
	session.AddHandler(b.onVoiceServerUpdate)

	return b, nil
}

// Run connects to Discord, registers slash commands, and blocks until shutdown.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("opening discord session: %w", err)
	}

	if b.session.State.User == nil {
		_ = b.session.Close()
		return fmt.Errorf("discord session user is not ready")
	}

	if err := b.registerCommands(); err != nil {
		_ = b.session.Close()
		return fmt.Errorf("registering slash commands: %w", err)
	}

	if b.music != nil {
		b.music.SetSession(b.session)
	}

	if b.linkdave != nil {
		b.linkdave.SetBotUserID(b.session.State.User.ID)

		connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := b.linkdave.Connect(connectCtx)
		cancel()
		if err != nil {
			_ = b.session.Close()
			return fmt.Errorf("connecting to linkdave: %w", err)
		}
	}

	b.logger.Info("bot is online", "user", b.session.State.User.Username)

	<-ctx.Done()

	b.logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if b.linkdave != nil {
		b.linkdave.DisconnectAll(shutdownCtx)
		_ = b.linkdave.Close()
	}
	if b.streamProxy != nil {
		_ = b.streamProxy.Close()
	}

	if err := b.session.Close(); err != nil {
		return fmt.Errorf("closing discord session: %w", err)
	}

	return nil
}

// WaitForSignal returns a context cancelled on SIGINT or SIGTERM.
func WaitForSignal(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-sigCh:
			slog.Info("received signal", "signal", sig.String())
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	return ctx, cancel
}

func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.registry.HandleInteraction(context.Background(), s, i)
}

func (b *Bot) onVoiceStateUpdate(_ *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	if b.linkdave == nil {
		return
	}
	b.linkdave.HandleVoiceStateUpdate(vs)
}

func (b *Bot) onVoiceServerUpdate(_ *discordgo.Session, vs *discordgo.VoiceServerUpdate) {
	if b.linkdave == nil {
		return
	}
	b.linkdave.HandleVoiceServerUpdate(vs)
}

func (b *Bot) registerCommands() error {
	appID := b.session.State.User.ID
	defs := b.registry.All()

	if b.cfg.DiscordGuild != "" {
		for _, def := range defs {
			_, err := b.session.ApplicationCommandCreate(appID, b.cfg.DiscordGuild, def)
			if err != nil {
				return fmt.Errorf("creating guild command /%s: %w", def.Name, err)
			}
		}
		b.logger.Info("registered guild slash commands", "guild_id", b.cfg.DiscordGuild, "count", len(defs))
		return nil
	}

	for _, def := range defs {
		_, err := b.session.ApplicationCommandCreate(appID, "", def)
		if err != nil {
			return fmt.Errorf("creating global command /%s: %w", def.Name, err)
		}
	}
	b.logger.Info("registered global slash commands", "count", len(defs))
	return nil
}
