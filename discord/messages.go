package discord

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/id"
	"discordbot/constants/zapkey"
	"discordbot/spotify"
	"discordbot/utils/ctxutil"
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

	// Zap logging Fields
	_, fields := ctxutil.WithZapFields(
		context.Background(),
		zap.String(zapkey.ChannelID, m.ChannelID),
		zap.String(zapkey.ID, m.ID),
		zap.String(zapkey.Type, "message"),
	)

	// Validate message
	logger.Info("Handling message", fields...)
	if err := validateMessage(m); err != nil {
		logger.With(zap.Error(err)).Error("invalid message", fields...)
		return
	}

	_, fields = ctxutil.WithZapFields(
		context.Background(),
		zap.String(zapkey.Content, m.Content),
		zap.String(zapkey.User, m.Author.Username),
		zap.String(zapkey.UserID, m.Author.ID),
	)

	// Log full message data if verbose logs are enabled
	verboseLogsEnabled, err := strconv.ParseBool(os.Getenv(envvar.VerboseLogsEnabled))
	if err != nil {
		logger.With(zap.Error(err)).Warn("Failed to parse verbose logs enabled", fields...)
	}
	if verboseLogsEnabled {
		logger.With(zap.Any(zapkey.Message, m)).Info("Full message data", fields...)
	}

	// TODO: Add message handling logic here
	if m.Author.Bot {
		logger.With(zap.String(zapkey.User, m.Author.Username)).Debug("ignoring bot message", fields...)
		return
	}

	logger.Info("Received message", fields...)

	if slices.Contains(h.channels(), m.ChannelID) {
		logger.Info("Received message from test channel", fields...)

		// Reply to the message
		reply := fmt.Sprintf("Echo: %s", m.Content)
		_, err := s.ChannelMessageSendReply(m.ChannelID, reply, m.Reference())
		if err != nil {
			logger.With(zap.Error(err)).Error("Failed to send reply", fields...)
		} else {
			logger.With(zap.String(zapkey.Reply, reply)).Info("Sent reply", fields...)
		}
	}

	// TODO This is sloppy, clean it up when we got it working
	tracks, ok := spotify.ExtractTracks(m.Content)
	if ok {
		logger.With(zap.Int(zapkey.Count, len(tracks)), zap.Strings(zapkey.Tracks, tracks)).Info("Found Spotify tracks", fields...)
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
