package discord

import (
	"fmt"
	"slices"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/channelid"
	"discordbot/spotify"
)

type MessageHandler struct {
}

// TODO: Make this configurable
func (h *MessageHandler) channels() []string {
	return []string{channelid.Test}
}

func (h *MessageHandler) String() string {
	return "MessageHandler"
}

func (h *MessageHandler) Add(session *discordgo.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	session.AddHandler(h.Handle)
	return nil
}

func (h *MessageHandler) Handle(s *discordgo.Session, m *discordgo.MessageCreate) {
	// TODO: Add message handling logic here
	if s == nil {
		zap.L().Error("session is nil")
		return
	}
	zap.L().Info("Handling message", zap.Any("message", m))
	if err := validateMessage(m); err != nil {
		zap.L().Error("invalid message", zap.Error(err))
		return
	}
	// TODO: Add message handling logic here
	if m.Author.Bot {
		zap.L().Debug("ignoring bot message", zap.String("user", m.Author.Username))
		return
	}

	zap.L().Info("Received message", zap.String("content", m.Content), zap.Any("message", m))

	if slices.Contains(h.channels(), m.ChannelID) {
		zap.L().Info("Received message from test channel")

		// Reply to the message
		reply := fmt.Sprintf("Replying to: %s", m.Content)
		_, err := s.ChannelMessageSendReply(m.ChannelID, reply, m.Reference())
		if err != nil {
			zap.L().Error("Failed to send reply", zap.Error(err))
		} else {
			zap.L().Info("Sent reply", zap.String("reply", reply))
		}
	}

	// TODO This is sloppy, clean it up when we got it working
	tracks, ok := spotify.ExtractTracks(m.Content)
	if ok {
		zap.L().Info("Found Spotify tracks", zap.Int("count", len(tracks)), zap.Strings("tracks", tracks))
	}
}

func validateMessage(m *discordgo.MessageCreate) error {
	if m == nil {
		return fmt.Errorf("message create is nil")
	}
	if m.Message == nil {
		return fmt.Errorf("message is nil")
	}
	if m.Content == "" {
		return fmt.Errorf("message content is empty")
	}
	if m.Author == nil {
		return fmt.Errorf("message author is nil")
	}
	return nil
}
