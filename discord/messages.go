package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/zapkey"
	"discordbot/discord/channel"
	"discordbot/log"
	"discordbot/spotify/track"
	"discordbot/utils/ctxutil"
)

// --- Constants ---

const (
	ActionReply               = "Reply"
	ActionAddTracksToPlaylist = "Add Tracks To Playlist"
)

// --- Interfaces ---

// TrackRouter routes Spotify track URLs to the appropriate playlist(s).
type TrackRouter interface {
	RouteTracksToPlaylist(ctx context.Context, userID string, trackURLs []string) error
}

// --- Message Sender ---

// SendMessage sends a message to a channel
func (c *Client) SendMessage(ctx context.Context, channelType string, message string) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("failed to validate discord client: %w", err)
	}
	channelID, ok := c.config.ChannelIDs[channel.NewType(channelType)]
	if !ok {
		return fmt.Errorf("no channel configured for type: %s", channelType)
	}

	_, err := c.session.ChannelMessageSend(channelID, message)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

// SendMessageWithButtons sends a message with interactive button components to a channel
// and returns the sent message's ID.
func (c *Client) SendMessageWithButtons(ctx context.Context, channelType string, message string, buttons []discordgo.Button) (string, error) {
	if err := c.Validate(); err != nil {
		return "", fmt.Errorf("failed to validate discord client: %w", err)
	}
	channelID, ok := c.config.ChannelIDs[channel.NewType(channelType)]
	if !ok {
		return "", fmt.Errorf("no channel configured for type: %s", channelType)
	}

	components := make([]discordgo.MessageComponent, len(buttons))
	for i, b := range buttons {
		components[i] = b
	}

	msg, err := c.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    message,
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: components}},
	})
	if err != nil {
		return "", fmt.Errorf("failed to send message with buttons: %w", err)
	}
	return msg.ID, nil
}

// --- Message Event Handlers ---

// MessageHandler handles message events
type MessageHandler struct {
	trackRouter TrackRouter
	actionIDs   ChannelActions // Map of channel IDs to actions to take for that channel
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(trackRouter TrackRouter, actions ChannelActions) *MessageHandler {
	return &MessageHandler{trackRouter: trackRouter, actionIDs: actions}
}

// String returns a string representation of the handler
func (h *MessageHandler) String() string {
	return "Message Handler"
}

// Add the handler to the session
func (h *MessageHandler) Add(session *discordgo.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	session.AddHandler(h.Handle)
	return nil
}

// Handle message events
func (h *MessageHandler) Handle(s *discordgo.Session, m *discordgo.MessageCreate) {
	if s == nil {
		logger.Error("session is nil")
		return
	}

	// Zap logging Fields
	ctx, fields := ctxutil.WithZapFields(
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

	ctx, fields = ctxutil.WithZapFields(
		context.Background(),
		zap.String(zapkey.UserName, m.Author.Username),
		zap.String(zapkey.UserID, m.Author.ID),
	)

	// Log full message data if verbose logs are enabled
	if log.VerboseLogsEnabled(ctx) {
		logger.With(zap.String(zapkey.Content, m.Content), zap.Any(zapkey.Message, m)).Info("Full message data", fields...)
	}

	// Ignore bot messages
	if m.Author.Bot {
		logger.With(zap.String(zapkey.UserName, m.Author.Username)).Debug("ignoring bot message", fields...)
		return
	}

	logger.Info("Received message", fields...)

	actionIDs, ok := h.actionIDs[m.ChannelID]
	if !ok {
		// Nothing to do, log a debug message and return
		logger.Debug("Received message for unknown channel", fields...)
		return
	}

	// List of actions to perform
	var actions []Action

	// Determine actions to perform
	for _, actionID := range actionIDs {
		switch actionID {
		case ActionReply:
			reply := &Reply{session: s, event: m}
			actions = append(actions, reply)
		case ActionAddTracksToPlaylist:
			addTracksToPlaylist := &AddTracksToPlaylist{session: s, event: m, trackRouter: h.trackRouter}
			actions = append(actions, addTracksToPlaylist)
		default:
			logger.With(zap.String(zapkey.Action, actionID)).Error("received unknown action", fields...)
		}
	}

	// Perform actions
	for _, action := range actions {
		action.Execute(ctx)
	}
}

// Reply handles replying to a message
type Reply struct {
	session  *discordgo.Session
	event    *discordgo.MessageCreate
	response string
}

// String returns a string representation of the action
func (r *Reply) String() string {
	return ActionReply
}

// Execute sends the reply
func (r *Reply) Execute(ctx context.Context) {
	fields := ctxutil.ZapFields(ctx)
	errMsg := "Failed to send reply"
	if r.session == nil {
		logger.With(zap.Error(fmt.Errorf("session is nil"))).Error(errMsg, fields...)
		return
	}
	if r.event == nil {
		logger.With(zap.Error(fmt.Errorf("message is nil"))).Error(errMsg, fields...)
		return
	}
	// If no response is set, default to echo
	if r.response == "" {
		r.response = fmt.Sprintf("Echo: %s", r.event.Content)
	}
	_, err := r.session.ChannelMessageSendReply(r.event.ChannelID, r.response, r.event.Reference())
	if err != nil {
		logger.With(zap.Error(err)).Error(errMsg, fields...)
		return
	}
	logger.With(zap.String(zapkey.Reply, r.response)).Info("Sent reply", fields...)
}

// validateMessage validates the received message
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

// AddTracksToPlaylist handles routing tracks to the appropriate playlist
type AddTracksToPlaylist struct {
	session     *discordgo.Session
	event       *discordgo.MessageCreate
	trackRouter TrackRouter
}

// String returns a string representation of the action
func (a *AddTracksToPlaylist) String() string {
	return ActionAddTracksToPlaylist
}

// Execute routes tracks to the appropriate playlist
func (a *AddTracksToPlaylist) Execute(ctx context.Context) {
	fields := ctxutil.ZapFields(ctx)

	if err := a.Validate(); err != nil {
		logger.With(zap.Error(err)).Error("Failed to route tracks to playlist", fields...)
		return
	}

	// Extract track URLs from message
	trackURLs, ok := track.ExtractURLs(a.event.Content)
	if !ok {
		// Not an error, just not a message with Spotify tracks
		logger.Info("No tracks found in message", fields...)
		return
	}

	ctx, fields = ctxutil.WithZapFields(
		ctx,
		zap.Strings(zapkey.TrackURLs, trackURLs),
	)

	logger.With(zap.Int(zapkey.Count, len(trackURLs))).Info("Found Spotify tracks", fields...)

	if err := a.trackRouter.RouteTracksToPlaylist(ctx, a.event.Author.ID, trackURLs); err != nil {
		logger.With(zap.Error(err)).Error("Failed to route tracks to playlist", fields...)
		return
	}
}

// Validate validates the action
func (a *AddTracksToPlaylist) Validate() error {
	if a.trackRouter == nil {
		return fmt.Errorf("track router is nil")
	}
	if a.event == nil {
		return fmt.Errorf("message is nil")
	}
	if a.session == nil {
		return fmt.Errorf("session is nil")
	}
	return nil
}
