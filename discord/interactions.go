package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/id"
	"discordbot/constants/zapkey"
	"discordbot/utils/ctxutil"
	"discordbot/utils/stringutil"
)

// InteractionSessionHandler handles interactions
type InteractionSessionHandler struct {
	// TODO: Config
}

// NewInteractionSessionHandler creates a new interaction session handler
func NewInteractionSessionHandler() *InteractionSessionHandler {
	return &InteractionSessionHandler{}
}

// String returns a string representation of the interaction session handler
func (h *InteractionSessionHandler) String() string {
	return "Interaction Session Handler"
}

// Add adds the interaction session handler to the session
func (h *InteractionSessionHandler) Add(session *discordgo.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	session.AddHandler(h.Handle)
	return nil
}

// Handle interaction events
func (h *InteractionSessionHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if s == nil {
		logger.Error("session is nil")
		return
	}
	if i == nil {
		logger.Error("interaction is nil")
		return
	}

	_, fields := ctxutil.WithZapFields(
		context.Background(),
		zap.String(zapkey.Type, i.Type.String()),
		zap.String(zapkey.ID, i.ID),
	)

	logger.Info("Received interaction", fields...)

	// Determine interaction responder function
	switch i.Type {
	case discordgo.InteractionPing:
		h.ping(s, i)
	case discordgo.InteractionApplicationCommand:
		h.slashCommand(s, i)
	default:
		logger.Error("no responder for interaction type", fields...)
	}
}

// ping handles ping interactions
func (h *InteractionSessionHandler) ping(s *discordgo.Session, i *discordgo.InteractionCreate) {
	logger.Info("Handling ping interaction")

	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponsePong,
		Data: nil,
	}

	if err := s.InteractionRespond(i.Interaction, &response); err != nil {
		logger.Error("failed to respond to ping", zap.Error(err))
	}
}

// slashCommand handles slash command interactions
func (h *InteractionSessionHandler) slashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	logger.Info("Handling slash command", zap.String(zapkey.Command, data.Name))

	switch data.Name {
	case testCommand:
		h.testCommand(s, i)
	case challengeCommand:
		h.challengeCommand(s, i)
	default:
		logger.Error("unknown slash command", zap.String(zapkey.Command, data.Name))
	}
}

// testCommand handles the /test slash command interaction
func (h *InteractionSessionHandler) testCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var userID string
	if i.User != nil {
		userID = i.User.ID
	} else if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	}

	// TODO: Make this configurable
	var message string
	switch userID {
	case id.UserIDGio:
		message = "GIOGIOGIO"
	case id.UserIDRehan:
		message = "REHREHREH"
	default:
		message = "Test message"
	}

	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	}

	if err := s.InteractionRespond(i.Interaction, &response); err != nil {
		logger.Error("failed to respond to test command", zap.Error(err))
	}
}

// challengeCommand handles the /challenge slash command interaction
func (h *InteractionSessionHandler) challengeCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	msg := "Challenge me once you're worthy."
	data := i.ApplicationCommandData()
	if len(data.Options) > 0 {
		choice := data.Options[0].StringValue()
		msg = fmt.Sprintf(
			"%s? Really? You think you can defeat me with %s? %s",
			stringutil.ToTitleCase(choice),
			strings.ToUpper(choice),
			msg,
		)
	}
	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	}

	if err := s.InteractionRespond(i.Interaction, &response); err != nil {
		logger.Error("failed to respond to challenge command", zap.Error(err))
	}
}
