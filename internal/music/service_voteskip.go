package music

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// VoteSkip records a listener vote to skip the current track.
func (s *Service) VoteSkip(ctx context.Context, session *discordgo.Session, botUserID, guildID, userID string) (current, needed int, skipped bool, err error) {
	if userID == "" {
		return 0, 0, false, fmt.Errorf("could not identify voter")
	}

	needed = voteSkipThreshold(session, guildID, botUserID)
	s.voteMu.Lock()
	if s.voteSkip == nil {
		s.voteSkip = make(map[string]map[string]struct{})
	}
	if s.voteSkip[guildID] == nil {
		s.voteSkip[guildID] = make(map[string]struct{})
	}
	s.voteSkip[guildID][userID] = struct{}{}
	current = len(s.voteSkip[guildID])
	s.voteMu.Unlock()

	if current < needed {
		s.emitPlayback(guildID, true)
		return current, needed, false, nil
	}

	s.clearVoteSkip(guildID)
	_, ok, err := s.Skip(ctx, guildID)
	return needed, needed, ok, err
}

// VoteSkipStatus returns current votes and the threshold for the active track.
func (s *Service) VoteSkipStatus(session *discordgo.Session, botUserID, guildID string) (current, needed int) {
	needed = voteSkipThreshold(session, guildID, botUserID)
	s.voteMu.Lock()
	defer s.voteMu.Unlock()
	if s.voteSkip == nil {
		return 0, needed
	}
	return len(s.voteSkip[guildID]), needed
}

func (s *Service) clearVoteSkip(guildID string) {
	s.voteMu.Lock()
	delete(s.voteSkip, guildID)
	s.voteMu.Unlock()
}

func voteSkipThreshold(session *discordgo.Session, guildID, botUserID string) int {
	botChannel := BotVoiceChannel(session, guildID, botUserID)
	if botChannel == "" || session == nil {
		return 1
	}

	guild, err := session.State.Guild(guildID)
	if err != nil {
		return 1
	}

	listeners := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID != botChannel || vs.UserID == botUserID {
			continue
		}
		if member, err := session.State.Member(guildID, vs.UserID); err == nil && member != nil && member.User != nil && member.User.Bot {
			continue
		}
		listeners++
	}

	if listeners <= 1 {
		return 1
	}
	return (listeners + 1) / 2
}
