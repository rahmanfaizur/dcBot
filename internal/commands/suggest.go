package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/faizur/mybot/internal/ai"
	"github.com/faizur/mybot/internal/music"
)

const suggestTTL = 30 * time.Minute

type suggestPack struct {
	tracks  []ai.TrackPick
	expires time.Time
}

// SuggestStore keeps short-lived AI track picks for play buttons.
type SuggestStore struct {
	mu   sync.Mutex
	byID map[string]suggestPack
}

func NewSuggestStore() *SuggestStore {
	return &SuggestStore{byID: make(map[string]suggestPack)}
}

func (st *SuggestStore) Put(tracks []ai.TrackPick) string {
	if st == nil || len(tracks) == 0 {
		return ""
	}
	id := randomSuggestID(4)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.gcLocked()
	st.byID[id] = suggestPack{tracks: append([]ai.TrackPick(nil), tracks...), expires: time.Now().Add(suggestTTL)}
	return id
}

func (st *SuggestStore) Get(id string, index int) (ai.TrackPick, bool) {
	if st == nil || id == "" || index < 0 {
		return ai.TrackPick{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.gcLocked()
	pack, ok := st.byID[id]
	if !ok || index >= len(pack.tracks) {
		return ai.TrackPick{}, false
	}
	return pack.tracks[index], true
}

func (st *SuggestStore) gcLocked() {
	now := time.Now()
	for id, pack := range st.byID {
		if now.After(pack.expires) {
			delete(st.byID, id)
		}
	}
}

func randomSuggestID(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func suggestButtons(packID string, tracks []ai.TrackPick) []discordgo.MessageComponent {
	if packID == "" || len(tracks) == 0 {
		return nil
	}
	var rows []discordgo.MessageComponent
	for i, t := range tracks {
		label := t.Title
		if t.Artist != "" {
			label = t.Artist + " — " + t.Title
		}
		label = truncateRunes(label, 75)
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "▶ " + label,
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("music:aiplay:%s:%d", packID, i),
				},
			},
		})
	}
	return rows
}

func editDeferredAI(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	edit := &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{embed}}
	if len(components) > 0 {
		edit.Components = &components
	}
	_, err := s.InteractionResponseEdit(i.Interaction, edit)
	return err
}

func (p *PlayerPanel) handleAIPlay(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, customID string) error {
	parts := strings.Split(customID, ":")
	// music:aiplay:<id>:<idx>
	if len(parts) < 4 || p.suggest == nil {
		return respondEphemeral(s, i, "That suggestion expired. Ask again with `/ask` or `/vibe`.")
	}
	packID := parts[2]
	idx := 0
	if _, err := fmt.Sscanf(parts[3], "%d", &idx); err != nil {
		return respondEphemeral(s, i, "Invalid suggestion button.")
	}
	pick, ok := p.suggest.Get(packID, idx)
	if !ok {
		return respondEphemeral(s, i, "That suggestion expired. Ask again with `/ask` or `/vibe`.")
	}

	channelID := music.MemberVoiceChannel(s, i)
	if channelID == "" {
		return respondEphemeral(s, i, "Join a voice channel first, then tap play.")
	}
	if err := deferChannel(s, i); err != nil {
		return err
	}

	playCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	_ = editDeferredEmbed(s, i, loadingEmbed(pick.Query))
	if err := p.svc.EnsureVoice(playCtx, s, botUserID(s), i.GuildID, channelID); err != nil {
		return editDeferredEmbed(s, i, voiceErrorEmbed(music.FriendlyControlError(err)))
	}
	_ = editDeferredEmbed(s, i, preparingEmbed(pick.Query))

	result, err := p.svc.Enqueue(playCtx, i.GuildID, pick.Query, requesterID(i), requesterName(i))
	if err != nil {
		return editDeferredEmbed(s, i, playErrorEmbed(music.DescribePlaybackError(err)))
	}
	if len(result.Tracks) == 0 {
		return editDeferredEmbed(s, i, playErrorEmbed(music.DescribePlaybackError(fmt.Errorf("could not resolve that track"))))
	}
	if result.Started {
		return p.PublishFromInteraction(s, i, result.Tracks[0], requesterName(i))
	}
	p.UpdateGuildPanel(s, i.GuildID)
	return editDeferredEmbed(s, i, queuedEmbed(result.Tracks[0], p.svc.QueuePosition(i.GuildID), requesterName(i)))
}

func respondAIReply(s *discordgo.Session, i *discordgo.InteractionCreate, title string, reply ai.Reply, store *SuggestStore) error {
	embed := aiEmbed(title, reply.Message)
	var components []discordgo.MessageComponent
	if len(reply.Tracks) > 0 && store != nil {
		id := store.Put(reply.Tracks)
		components = suggestButtons(id, reply.Tracks)
		if len(components) > 0 {
			embed.Description = reply.Message + "\n\n*Tap a button below to play it in your voice channel.*"
		}
	}
	return editDeferredAI(s, i, embed, components)
}

func askSuggestSystem() string {
	return strings.TrimSpace(`
You are FR, a Discord music bot from FR Labs that CAN play songs when the user clicks buttons under your reply.
Always answer with ONLY a JSON object (no markdown fences):
{"message":"friendly text under 120 words","tracks":[{"title":"Song","artist":"Artist","query":"Artist Song youtube search"}]}
Rules:
- If the user wants music, playlists, recommendations, or "play X", include 3-5 real tracks in tracks.
- query must be a good YouTube search string (artist + title).
- If the question is not about music, set tracks to [].
- Never say you cannot play music on Discord — buttons handle playback.
- Do not invent full lyrics. Keep message warm and concise.
`)
}

func vibeSuggestSystem() string {
	return strings.TrimSpace(`
You are FR, a Discord music companion. You CAN queue songs via buttons under your message.
Reply with ONLY JSON (no markdown fences):
{"message":"short vibe reply under 120 words","tracks":[{"title":"Song","artist":"Artist","query":"Artist Song"}]}
Include 1-5 tracks when recommending or when the user asks what to play next. Use tracks=[] if no suggestion.
Never claim you cannot play music.
`)
}
