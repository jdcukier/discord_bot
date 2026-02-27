package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// ReadyHandler fires when the bot comes online (Discord gateway READY event)
type ReadyHandler struct {
	channelID string
	message   string
}

// NewReadyHandler creates a new ready handler
func NewReadyHandler(channelID, message string) *ReadyHandler {
	return &ReadyHandler{channelID: channelID, message: message}
}

// String returns a string representation of the handler
func (h *ReadyHandler) String() string {
	return "Ready Handler"
}

// Add registers the ready handler with the session
func (h *ReadyHandler) Add(session *discordgo.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		if _, err := s.ChannelMessageSend(h.channelID, h.message); err != nil {
			logger.Error("failed to send startup message: " + err.Error())
		}
		if err := s.UpdateStatusComplex(discordgo.UpdateStatusData{
			Activities: []*discordgo.Activity{{
				Name: "BANGers",
				Type: discordgo.ActivityTypeListening,
			}},
		}); err != nil {
			logger.Error("failed to set presence: " + err.Error())
		}
	})
	return nil
}
