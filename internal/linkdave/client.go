package linkdave

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

// Client connects discordgo voice events to a Linkdave node.
type Client struct {
	logger      *slog.Logger
	botUserID   string
	nodeURL     string
	password    string
	updateVoice func(guildID, channelID string, mute, deaf bool) error

	rest *RESTClient

	mu      sync.RWMutex
	conn    *websocket.Conn
	session string
	players map[string]*Player
	closed  bool
}

// Config configures a Linkdave client.
type Config struct {
	NodeURL     string
	Password    string
	BotUserID   string
	Logger      *slog.Logger
	UpdateVoice func(guildID, channelID string, mute, deaf bool) error
}

// NewClient creates an unconnected Linkdave client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.NodeURL == "" {
		return nil, fmt.Errorf("linkdave node url is required")
	}
	if cfg.UpdateVoice == nil {
		return nil, fmt.Errorf("update voice callback is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	rest, err := NewRESTClient(cfg.NodeURL, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("creating linkdave rest client: %w", err)
	}

	return &Client{
		logger:      cfg.Logger,
		botUserID:   cfg.BotUserID,
		nodeURL:     cfg.NodeURL,
		password:    cfg.Password,
		updateVoice: cfg.UpdateVoice,
		rest:        rest,
		players:     make(map[string]*Player),
	}, nil
}

// Connect opens the Linkdave WebSocket and waits for OpReady.
func (c *Client) Connect(ctx context.Context) error {
	wsURL, err := buildWSURL(c.nodeURL, c.password)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dialing linkdave websocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	readyCh := make(chan error, 1)
	go c.readLoop(readyCh)

	select {
	case err := <-readyCh:
		if err != nil {
			_ = c.Close()
			return err
		}
		c.logger.Info("connected to linkdave", "session_id", c.getSessionID())
		return nil
	case <-ctx.Done():
		_ = c.Close()
		return fmt.Errorf("connecting to linkdave: %w", ctx.Err())
	case <-time.After(10 * time.Second):
		_ = c.Close()
		return fmt.Errorf("connecting to linkdave: timed out waiting for ready")
	}
}

// Close shuts down the Linkdave WebSocket connection.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		return conn.Close()
	}
	return nil
}

// Player returns the guild player, creating it when needed.
func (c *Client) Player(guildID string) *Player {
	c.mu.Lock()
	defer c.mu.Unlock()

	if player, ok := c.players[guildID]; ok {
		return player
	}

	player := &Player{
		client:  c,
		guildID: guildID,
		state:   PlayerStateIdle,
	}
	c.players[guildID] = player
	return player
}

// HandleVoiceStateUpdate forwards the bot's voice state update to the guild player.
func (c *Client) HandleVoiceStateUpdate(vs *discordgo.VoiceStateUpdate) {
	if vs == nil || vs.UserID != c.botUserID {
		return
	}
	c.Player(vs.GuildID).handleVoiceStateUpdate(vs.ChannelID, vs.SessionID)
}

// HandleVoiceServerUpdate forwards Discord's voice server update to the guild player.
func (c *Client) HandleVoiceServerUpdate(vs *discordgo.VoiceServerUpdate) {
	if vs == nil {
		return
	}
	c.Player(vs.GuildID).handleVoiceServerUpdate(VoiceServerEvent{
		Token:    vs.Token,
		GuildID:  vs.GuildID,
		Endpoint: vs.Endpoint,
	})
}

func (c *Client) getSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.session
}

func (c *Client) sendVoiceUpdate(data VoiceUpdateData) error {
	return c.sendWS(OpVoiceUpdate, data)
}

func (c *Client) sendVoiceStateUpdate(guildID, channelID string) error {
	deaf := channelID != ""
	return c.updateVoice(guildID, channelID, false, deaf)
}

func (c *Client) sendWS(op uint8, data any) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("linkdave websocket is not connected")
	}

	msg, err := json.Marshal(WSMessage{Op: op, Data: data})
	if err != nil {
		return fmt.Errorf("encoding websocket message: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		return fmt.Errorf("writing websocket message: %w", err)
	}
	return nil
}

func (c *Client) readLoop(readyCh chan error) {
	for {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			return
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			c.logger.Warn("linkdave websocket closed", "error", err)
			return
		}

		var envelope struct {
			Op   uint8           `json:"op"`
			Data json.RawMessage `json:"d"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			c.logger.Warn("ignoring malformed linkdave message", "error", err)
			continue
		}

		switch envelope.Op {
		case OpReady:
			var ready ReadyData
			if err := json.Unmarshal(envelope.Data, &ready); err != nil {
				readyCh <- fmt.Errorf("decoding linkdave ready: %w", err)
				return
			}
			c.mu.Lock()
			c.session = ready.SessionID
			c.mu.Unlock()
			readyCh <- nil
		case OpVoiceConnect:
			var data VoiceConnectData
			if err := json.Unmarshal(envelope.Data, &data); err == nil {
				c.logger.Info("linkdave voice connected", "guild_id", data.GuildID, "channel_id", data.ChannelID)
				c.Player(data.GuildID).onVoiceConnect()
			}
		case OpVoiceDisconnect:
			var data VoiceDisconnectData
			if err := json.Unmarshal(envelope.Data, &data); err == nil {
				c.logger.Info("linkdave voice disconnected", "guild_id", data.GuildID, "reason", data.Reason)
				c.Player(data.GuildID).onVoiceDisconnect(data.Reason)
			}
		case OpPlayerUpdate:
			var data PlayerUpdateData
			if err := json.Unmarshal(envelope.Data, &data); err == nil {
				c.Player(data.GuildID).onPlayerUpdate(data.State)
			}
		case OpTrackStart:
			var data TrackStartData
			if err := json.Unmarshal(envelope.Data, &data); err == nil {
				c.logger.Info("track started", "guild_id", data.GuildID, "url", data.Track.URL)
				c.Player(data.GuildID).onTrackStart(data.Track)
			}
		case OpTrackEnd:
			var data TrackEndData
			if err := json.Unmarshal(envelope.Data, &data); err == nil {
				c.logger.Info("track ended", "guild_id", data.GuildID, "reason", data.Reason)
				c.Player(data.GuildID).onTrackEnd(data)
			}
		case OpTrackError:
			var data struct {
				GuildID string    `json:"guild_id"`
				Track   TrackInfo `json:"track"`
				Error   string    `json:"error"`
			}
			if err := json.Unmarshal(envelope.Data, &data); err == nil {
				c.logger.Error("track error", "guild_id", data.GuildID, "error", data.Error)
			}
		case OpNodeDraining:
			c.logger.Warn("linkdave node draining")
		}
	}
}

func buildWSURL(nodeURL, password string) (string, error) {
	parsed, err := url.Parse(nodeURL)
	if err != nil {
		return "", fmt.Errorf("parsing linkdave url: %w", err)
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws"
	}
	query := parsed.Query()
	query.Set("node", "main")
	if password != "" {
		query.Set("password", password)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// SetTrackEndHook registers a callback fired after Linkdave reports track end.
func (p *Player) SetTrackEndHook(hook func(TrackEndData)) {
	p.mu.Lock()
	p.trackEndHook = hook
	p.mu.Unlock()
}

// GuildIDs returns guilds with allocated players.
func (c *Client) GuildIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]string, 0, len(c.players))
	for id := range c.players {
		ids = append(ids, id)
	}
	return ids
}

// DisconnectAll leaves voice in every guild during shutdown.
func (c *Client) DisconnectAll(ctx context.Context) {
	for _, guildID := range c.GuildIDs() {
		player := c.Player(guildID)
		if player.Connected() {
			if err := player.Disconnect(ctx); err != nil {
				c.logger.Warn("disconnecting guild voice", "guild_id", guildID, "error", err)
			}
		}
	}
}

// SetBotUserID sets the Discord bot user id used to filter voice state updates.
func (c *Client) SetBotUserID(id string) {
	c.botUserID = id
}
