package linkdave

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Player manages voice state and playback for one guild on a Linkdave node.
type Player struct {
	client    *Client
	guildID   string
	channelID string

	mu         sync.Mutex
	state      string
	current    *TrackInfo
	voiceReady bool
	connecting bool
	pending    *pendingVoice
	voiceState *voiceSession

	connectWaiters []chan error
	trackEndHook   func(TrackEndData)
}

type pendingVoice struct {
	channelID   string
	sessionID   string
	serverEvent *VoiceServerEvent
}

type voiceSession struct {
	channelID   string
	sessionID   string
	serverEvent VoiceServerEvent
}

// Connect joins a voice channel and waits for Linkdave to confirm DAVE voice setup.
func (p *Player) Connect(ctx context.Context, channelID string) error {
	if channelID == "" {
		return fmt.Errorf("channel id is required")
	}

	p.mu.Lock()
	if p.connecting {
		p.mu.Unlock()
		return fmt.Errorf("already connecting to voice")
	}
	p.connecting = true
	p.pending = nil
	p.channelID = channelID
	waiter := make(chan error, 1)
	p.connectWaiters = append(p.connectWaiters, waiter)
	p.mu.Unlock()

	if err := p.client.sendVoiceStateUpdate(p.guildID, channelID); err != nil {
		p.finishConnect(err)
		return fmt.Errorf("updating voice state: %w", err)
	}

	select {
	case err := <-waiter:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		_ = p.Disconnect(context.Background())
		return fmt.Errorf("connecting voice: %w", ctx.Err())
	case <-time.After(connectTimeout(ctx)):
		_ = p.Disconnect(context.Background())
		return fmt.Errorf("connecting voice: timed out waiting for linkdave")
	}
}

func connectTimeout(ctx context.Context) time.Duration {
	const defaultTimeout = 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < defaultTimeout {
			return remaining
		}
	}
	return defaultTimeout
}

// Disconnect leaves the current voice channel.
func (p *Player) Disconnect(ctx context.Context) error {
	p.mu.Lock()
	hadVoice := p.voiceState != nil
	p.mu.Unlock()

	if err := p.client.sendVoiceStateUpdate(p.guildID, ""); err != nil {
		return fmt.Errorf("leaving voice channel: %w", err)
	}

	if hadVoice && p.client.getSessionID() != "" {
		if err := p.client.rest.disconnect(ctx, p.client.getSessionID(), p.guildID); err != nil {
			return fmt.Errorf("disconnecting linkdave player: %w", err)
		}
	}

	p.resetVoice()
	return nil
}

// Play streams a direct audio URL through Linkdave.
func (p *Player) Play(ctx context.Context, streamURL, requesterID string) error {
	sessionID := p.client.getSessionID()
	if sessionID == "" {
		return fmt.Errorf("linkdave is not connected")
	}

	if err := p.client.rest.play(ctx, sessionID, p.guildID, PlayRequest{
		URL:         streamURL,
		RequesterID: requesterID,
	}); err != nil {
		return fmt.Errorf("starting playback: %w", err)
	}

	return nil
}

// Pause pauses the current track.
func (p *Player) Pause(ctx context.Context) error {
	sessionID := p.client.getSessionID()
	if sessionID == "" {
		return fmt.Errorf("linkdave is not connected")
	}
	if err := p.client.rest.pause(ctx, sessionID, p.guildID); err != nil {
		return fmt.Errorf("pausing playback: %w", err)
	}
	return nil
}

// Resume resumes paused playback.
func (p *Player) Resume(ctx context.Context) error {
	sessionID := p.client.getSessionID()
	if sessionID == "" {
		return fmt.Errorf("linkdave is not connected")
	}
	if err := p.client.rest.resume(ctx, sessionID, p.guildID); err != nil {
		return fmt.Errorf("resuming playback: %w", err)
	}
	return nil
}

// Stop stops playback without leaving voice.
func (p *Player) Stop(ctx context.Context) error {
	sessionID := p.client.getSessionID()
	if sessionID == "" {
		return fmt.Errorf("linkdave is not connected")
	}
	if err := p.client.rest.stop(ctx, sessionID, p.guildID); err != nil {
		return fmt.Errorf("stopping playback: %w", err)
	}
	return nil
}

// ChannelID returns the connected voice channel, if any.
func (p *Player) ChannelID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.channelID
}

// State returns the latest player state from Linkdave.
func (p *Player) State() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Current returns the track currently reported by Linkdave.
func (p *Player) Current() *TrackInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil {
		return nil
	}
	copy := *p.current
	return &copy
}

// Connected reports whether Linkdave has an active voice session for this guild.
func (p *Player) Connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.voiceState != nil
}

func (p *Player) handleVoiceStateUpdate(channelID, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if channelID == "" {
		p.resetVoiceLocked()
		return
	}

	if p.pending == nil {
		p.pending = &pendingVoice{}
	}
	p.pending.channelID = channelID
	p.pending.sessionID = sessionID
	p.tryConnectLinkdaveLocked()
}

func (p *Player) handleVoiceServerUpdate(event VoiceServerEvent) {
	if event.Endpoint == "" {
		p.mu.Lock()
		p.resetVoiceLocked()
		p.mu.Unlock()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pending == nil {
		p.pending = &pendingVoice{}
	}
	p.pending.serverEvent = &event
	p.tryConnectLinkdaveLocked()
}

func (p *Player) tryConnectLinkdaveLocked() {
	if p.pending == nil || p.pending.channelID == "" || p.pending.sessionID == "" || p.pending.serverEvent == nil {
		return
	}

	p.channelID = p.pending.channelID
	p.voiceState = &voiceSession{
		channelID:   p.pending.channelID,
		sessionID:   p.pending.sessionID,
		serverEvent: *p.pending.serverEvent,
	}
	p.pending = nil

	if err := p.client.sendVoiceUpdate(VoiceUpdateData{
		ClientID:  p.client.botUserID,
		GuildID:   p.guildID,
		ChannelID: p.voiceState.channelID,
		SessionID: p.voiceState.sessionID,
		Event:     p.voiceState.serverEvent,
	}); err != nil {
		p.failConnectLocked(fmt.Errorf("sending voice update to linkdave: %w", err))
	}
}

func (p *Player) onVoiceConnect() {
	p.mu.Lock()
	p.voiceReady = true
	p.connecting = false
	p.state = PlayerStateIdle
	waiters := p.connectWaiters
	p.connectWaiters = nil
	p.mu.Unlock()

	for _, ch := range waiters {
		ch <- nil
		close(ch)
	}
}

func (p *Player) onVoiceDisconnect(reason string) {
	p.mu.Lock()
	waiters := p.connectWaiters
	p.connectWaiters = nil
	connecting := p.connecting
	p.mu.Unlock()

	if connecting && reason != DisconnectReasonRequested {
		err := fmt.Errorf("voice connection failed: %s", reason)
		for _, ch := range waiters {
			ch <- err
			close(ch)
		}
	}

	p.resetVoice()
}

func (p *Player) onPlayerUpdate(state string) {
	p.mu.Lock()
	p.state = state
	p.mu.Unlock()
}

func (p *Player) onTrackStart(track TrackInfo) {
	p.mu.Lock()
	copy := track
	p.current = &copy
	p.state = PlayerStatePlaying
	p.mu.Unlock()
}

func (p *Player) onTrackEnd(data TrackEndData) {
	p.mu.Lock()
	if data.Reason != TrackEndStopped && data.Reason != TrackEndReplaced {
		p.current = nil
		if p.state != PlayerStatePaused {
			p.state = PlayerStateIdle
		}
	}
	hook := p.trackEndHook
	p.mu.Unlock()

	if hook != nil {
		hook(data)
	}
}

func (p *Player) finishConnect(err error) {
	p.mu.Lock()
	waiters := p.connectWaiters
	p.connectWaiters = nil
	p.connecting = false
	p.mu.Unlock()

	for _, ch := range waiters {
		ch <- err
		close(ch)
	}
}

func (p *Player) failConnectLocked(err error) {
	waiters := p.connectWaiters
	p.connectWaiters = nil
	p.connecting = false
	p.resetVoiceLocked()

	for _, ch := range waiters {
		ch <- err
		close(ch)
	}
}

func (p *Player) resetVoice() {
	p.mu.Lock()
	p.resetVoiceLocked()
	p.mu.Unlock()
}

func (p *Player) resetVoiceLocked() {
	p.channelID = ""
	p.voiceState = nil
	p.pending = nil
	p.voiceReady = false
	p.connecting = false
	p.current = nil
	p.state = PlayerStateIdle
}

// ResetVoice clears local voice state without contacting Discord.
func (p *Player) ResetVoice() {
	p.resetVoice()
}
