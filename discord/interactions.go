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

// InteractionRouter handles Spotify interaction callbacks triggered by Discord button clicks and modal submissions.
type InteractionRouter interface {
	HandlePlaylistSelection(ctx context.Context, messageID string, selectedPlaylistID string) error
	HandleNewPlaylistConfirm(ctx context.Context, messageID string, playlistName string) error
	HandleInteractionCancel(ctx context.Context, messageID string)
}

// InteractionSessionHandler handles interactions
type InteractionSessionHandler struct {
	interactionRouter InteractionRouter
}

// NewInteractionSessionHandler creates a new interaction session handler
func NewInteractionSessionHandler(router InteractionRouter) *InteractionSessionHandler {
	return &InteractionSessionHandler{interactionRouter: router}
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

	switch i.Type {
	case discordgo.InteractionPing:
		h.ping(s, i)
	case discordgo.InteractionApplicationCommand:
		h.slashCommand(s, i)
	case discordgo.InteractionMessageComponent:
		h.componentInteraction(s, i)
	case discordgo.InteractionModalSubmit:
		h.modalSubmit(s, i)
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

// componentInteraction handles button and select-menu interactions from playlist routing prompts.
func (h *InteractionSessionHandler) componentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	customID := data.CustomID

	logger.Info("Handling component interaction", zap.String("custom_id", customID))

	messageID := ""
	if i.Message != nil {
		messageID = i.Message.ID
	}

	// The "new_playlist" button responds with a modal — it must NOT send a deferred
	// update first, since only one InteractionRespond call is allowed per interaction.
	if customID == "new_playlist" {
		modalCustomID := "new_playlist_modal:" + messageID
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: modalCustomID,
				Title:    "New Playlist",
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.TextInput{
								CustomID:    "playlist_name",
								Label:       "Playlist name",
								Style:       discordgo.TextInputShort,
								Required:    true,
								MinLength:   1,
								MaxLength:   100,
								Placeholder: "My Awesome Playlist",
							},
						},
					},
				},
			},
		}); err != nil {
			logger.Error("failed to send new-playlist modal", zap.Error(err))
		}
		return
	}

	// For all other buttons: acknowledge immediately to avoid Discord's 3-second timeout,
	// then perform the Spotify API work.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		logger.Error("failed to acknowledge component interaction", zap.Error(err))
		return
	}

	if h.interactionRouter == nil {
		logger.Warn("no interaction router configured")
		return
	}

	ctx := context.Background()

	switch {
	case strings.HasPrefix(customID, "playlist_select:"):
		// custom_id format: "playlist_select:<playlistID>"
		playlistID := strings.TrimPrefix(customID, "playlist_select:")
		if err := h.interactionRouter.HandlePlaylistSelection(ctx, messageID, playlistID); err != nil {
			logger.Error("HandlePlaylistSelection failed", zap.Error(err))
		}

	case customID == "playlist_select_all":
		if err := h.interactionRouter.HandlePlaylistSelection(ctx, messageID, "all"); err != nil {
			logger.Error("HandlePlaylistSelection (all) failed", zap.Error(err))
		}

	case customID == "skip_playlist":
		h.interactionRouter.HandleInteractionCancel(ctx, messageID)

	default:
		logger.Warn("unknown component custom_id", zap.String("custom_id", customID))
	}
}

// modalSubmit handles modal submission for new playlist creation.
func (h *InteractionSessionHandler) modalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	customID := data.CustomID

	logger.Info("Handling modal submit", zap.String("custom_id", customID))

	// Acknowledge immediately
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		logger.Error("failed to acknowledge modal submit", zap.Error(err))
		return
	}

	if !strings.HasPrefix(customID, "new_playlist_modal:") {
		logger.Warn("unknown modal custom_id", zap.String("custom_id", customID))
		return
	}

	messageID := strings.TrimPrefix(customID, "new_playlist_modal:")

	// Extract playlist name from the modal's text input
	var playlistName string
	for _, row := range data.Components {
		actionsRow, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range actionsRow.Components {
			input, ok := comp.(*discordgo.TextInput)
			if ok && input.CustomID == "playlist_name" {
				playlistName = input.Value
			}
		}
	}

	if playlistName == "" {
		logger.Warn("playlist name was empty in modal submit")
		return
	}

	if h.interactionRouter == nil {
		logger.Warn("no interaction router configured")
		return
	}

	ctx := context.Background()
	if err := h.interactionRouter.HandleNewPlaylistConfirm(ctx, messageID, playlistName); err != nil {
		logger.Error("HandleNewPlaylistConfirm failed", zap.Error(err))
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
