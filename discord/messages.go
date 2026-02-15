package discord

import (
	"fmt"
	"slices"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/id"
	"discordbot/constants/zapkey"
	"discordbot/spotify"
)

type MessageHandler struct {
}

// TODO: Make this configurable
func (h *MessageHandler) channels() []string {
	return []string{id.ChannelIDTest}
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
		logger.Error("session is nil")
		return
	}
	logger.Info("Handling message", zap.Any("message", m))
	if err := validateMessage(m); err != nil {
		logger.Error("invalid message", zap.Error(err))
		return
	}
	// TODO: Add message handling logic here
	if m.Author.Bot {
		logger.Debug("ignoring bot message", zap.String(zapkey.User, m.Author.Username))
		return
	}

	logger.Info("Received message", zap.String(zapkey.Content, m.Content), zap.Any(zapkey.Message, m))

	if slices.Contains(h.channels(), m.ChannelID) {
		logger.Info("Received message from test channel")

		// Reply to the message
		reply := fmt.Sprintf("Replying to: %s", m.Content)
		_, err := s.ChannelMessageSendReply(m.ChannelID, reply, m.Reference())
		if err != nil {
			logger.Error("Failed to send reply", zap.Error(err))
		} else {
			logger.Info("Sent reply", zap.String(zapkey.Reply, reply))
		}
	}

	// TODO This is sloppy, clean it up when we got it working
	tracks, ok := spotify.ExtractTracks(m.Content)
	if ok {
		logger.Info("Found Spotify tracks", zap.Int(zapkey.Count, len(tracks)), zap.Strings(zapkey.Tracks, tracks))
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
