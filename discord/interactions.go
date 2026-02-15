package discord

import (
	"discordbot/constants/id"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/zapkey"
)

type InteractionHandler struct {
	*discordgo.Interaction
}

func NewInteractionHandler(r *http.Request) (*InteractionHandler, error) {
	interaction := &InteractionHandler{}
	if err := json.NewDecoder(r.Body).Decode(interaction); err != nil {
		return nil, fmt.Errorf("failed to decode interaction: %w", err)
	}

	return interaction, nil
}

func (h *InteractionHandler) Handle(w http.ResponseWriter) {
	if h == nil {
		logger.Error("interaction is nil")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Log interaction
	logger.Info("Received interaction",
		zap.String(zapkey.Type, fmt.Sprintf("%T", h.Type)),
		zap.String(zapkey.ID, h.ID),
		zap.String(zapkey.Name, h.ApplicationCommandData().Name))

	// Determine interaction responder function
	switch h.Type {
	case discordgo.InteractionPing:
		h.ping(w)
	case discordgo.InteractionApplicationCommand:
		h.handleSlashCommand(w)
	default:
		logger.Error("no responder for interaction type", zap.String(zapkey.Type, h.Type.String()))
		w.WriteHeader(http.StatusNotImplemented)
		return
	}
}

// TODO Make structs/interface to make this more flexible/testable

func (h *InteractionHandler) ping(w http.ResponseWriter) {
	logger.Info("Handling ping interaction")

	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponsePong,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("failed to encode ping response", zap.Error(err))
	}
}

func (h *InteractionHandler) handleSlashCommand(w http.ResponseWriter) {
	data := h.ApplicationCommandData()
	logger.Info("Handling slash command", zap.String(zapkey.Command, data.Name))

	switch data.Name {
	case testCommand:
		h.testCommand(w)
	case challengeCommand:
		h.challengeCommand(w)
	default:
		logger.Warn("Unknown slash command", zap.String(zapkey.Command, data.Name))
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *InteractionHandler) testCommand(w http.ResponseWriter) {
	logger.Info("Handling test command")

	var userID string
	if h.User != nil {
		userID = h.User.ID
	} else if h.Member != nil && h.Member.User != nil {
		userID = h.Member.User.ID
	}

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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", zap.Error(err))
	}
}

func (h *InteractionHandler) challengeCommand(w http.ResponseWriter) {
	logger.Info("Handling challenge command")

	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "You can challenge me once you're worthy.",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to encode response", zap.Error(err))
	}
}
