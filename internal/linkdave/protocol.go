// Package linkdave implements a Go client for the Linkdave audio server.
package linkdave

// Client opcodes sent to Linkdave over WebSocket.
const (
	OpVoiceUpdate   uint8 = 0
	OpPlayerMigrate uint8 = 1
)

// Server opcodes received from Linkdave over WebSocket.
const (
	OpReady           uint8 = 0
	OpVoiceConnect    uint8 = 1
	OpVoiceDisconnect uint8 = 2
	OpPlayerUpdate    uint8 = 3
	OpTrackStart      uint8 = 4
	OpTrackEnd        uint8 = 5
	OpTrackError      uint8 = 6
	OpStats           uint8 = 7
	OpNodeDraining    uint8 = 8
	OpMigrateReady    uint8 = 9
)

// Player states reported by Linkdave.
const (
	PlayerStateIdle       = "idle"
	PlayerStatePlaying    = "playing"
	PlayerStatePaused     = "paused"
	PlayerStateConnecting = "connecting"
)

// Track end reasons reported by Linkdave.
const (
	TrackEndFinished = "finished"
	TrackEndStopped  = "stopped"
	TrackEndReplaced = "replaced"
	TrackEndError    = "error"
)

// DisconnectReason describes why a voice connection ended.
const (
	DisconnectReasonRequested        = "requested"
	DisconnectReasonConnectionLost   = "connection_lost"
	DisconnectReasonConnectionFailed = "connection_failed"
	DisconnectReasonInactivity       = "inactivity"
)

// WSMessage is the envelope for Linkdave WebSocket payloads.
type WSMessage struct {
	Op   uint8 `json:"op"`
	Data any   `json:"d,omitempty"`
}

// ReadyData is sent when the WebSocket session is established.
type ReadyData struct {
	SessionID string `json:"session_id"`
	Resumed   bool   `json:"resumed"`
}

// VoiceServerEvent mirrors Discord's voice server update payload.
type VoiceServerEvent struct {
	Token    string `json:"token"`
	GuildID  string `json:"guild_id"`
	Endpoint string `json:"endpoint"`
}

// VoiceUpdateData connects Linkdave to a Discord voice session.
type VoiceUpdateData struct {
	ClientID  string           `json:"client_id"`
	GuildID   string           `json:"guild_id"`
	ChannelID string           `json:"channel_id"`
	SessionID string           `json:"session_id"`
	Event     VoiceServerEvent `json:"event"`
}

// TrackInfo describes a track known to Linkdave.
type TrackInfo struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Duration    int64  `json:"duration,omitempty"`
	RequesterID string `json:"requester_id,omitempty"`
}

// TrackStartData is emitted when playback begins.
type TrackStartData struct {
	GuildID string    `json:"guild_id"`
	Track   TrackInfo `json:"track"`
}

// TrackEndData is emitted when playback ends.
type TrackEndData struct {
	GuildID string    `json:"guild_id"`
	Track   TrackInfo `json:"track"`
	Reason  string    `json:"reason"`
}

// PlayerUpdateData reports player state changes.
type PlayerUpdateData struct {
	GuildID string `json:"guild_id"`
	State   string `json:"state"`
}

// VoiceConnectData confirms a successful voice connection.
type VoiceConnectData struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
}

// VoiceDisconnectData reports a voice disconnect.
type VoiceDisconnectData struct {
	GuildID string `json:"guild_id"`
	Reason  string `json:"reason,omitempty"`
}

// PlayRequest is the REST body for starting playback.
type PlayRequest struct {
	URL         string `json:"url"`
	StartTime   int64  `json:"start_time,omitempty"`
	RequesterID string `json:"requester_id,omitempty"`
}
