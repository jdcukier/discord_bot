package discord

import (
	"net/http"
	"encoding/json"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type InteractionHandler struct {
	interaction *discordgo.Interaction
}

func NewInteractionHandler(r *http.Request) (*InteractionHandler, error) {
	interaction := &discordgo.Interaction{}
	if err := json.NewDecoder(r.Body).Decode(interaction); err != nil {
		return nil, fmt.Errorf("failed to decode interaction: %w", err)
	}

	return &InteractionHandler{interaction: interaction}, nil
}

func (h *InteractionHandler) Handle(w http.ResponseWriter) {
	if h.interaction == nil {
		zap.L().Error("interaction is nil")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Log interaction
	zap.L().Info("Received interaction", 
		zap.String("type", fmt.Sprintf("%T", h.interaction.Type)),
		zap.String("id", h.interaction.ID),
		zap.String("name", h.interaction.ApplicationCommandData().Name))

	// Determine interaction responder function
	switch h.interaction.Type {
	case discordgo.InteractionPing:
		handlePing(w)
	case discordgo.InteractionApplicationCommand:
		handleSlashCommand(w, h.interaction)
	default:
		zap.L().Error("no responder for interaction type", zap.String("type", h.interaction.Type.String()))
		w.WriteHeader(http.StatusNotImplemented)
		return
	}
}

func handlePing(w http.ResponseWriter) {
	zap.L().Info("Handling ping interaction")
	
	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponsePong,
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func handleSlashCommand(w http.ResponseWriter, interaction *discordgo.Interaction) {
	data := interaction.ApplicationCommandData()
	zap.L().Info("Handling slash command", zap.String("command", data.Name))
	
	switch data.Name {
	case "test":
		handleTestCommand(w, interaction)
	default:
		zap.L().Warn("Unknown slash command", zap.String("command", data.Name))
		w.WriteHeader(http.StatusNotFound)
	}
}

func handleTestCommand(w http.ResponseWriter, interaction *discordgo.Interaction) {
	zap.L().Info("Handling test command")
	
	response := discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Test message",
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		zap.L().Error("Failed to encode response", zap.Error(err))
	}
}
