package discord

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/discord/channel"
	"discordbot/log"
	"discordbot/spotify/track"
	"discordbot/spotify/trigger"
	"discordbot/utils/ctxutil"
)

// --- Constants ---

const (
	ActionReply               = "Reply"
	ActionAddTracksToPlaylist = "Add Tracks To Playlist"
)

// --- Interfaces ---

// PlaylistAdder is an interface for adding tracks to playlists
type PlaylistAdder interface {
	AddTracksToPlaylist(ctx context.Context, userID string, trackURLs []string, playlistIDs ...string) error
}

// TrackRouter routes Spotify track URLs to the appropriate playlist(s), e.g. by genre.
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
	playlistAdder PlaylistAdder
	actionIDs     ChannelActions  // Map of channel IDs to actions to take for that channel
	triggers      *trigger.Config // Optional playlist triggers; nil adds to the default playlist only
	trackRouter   TrackRouter     // Optional router for messages without a known trigger; nil adds to the default playlist
}

// MessageHandlerOption overrides a default message handler value
type MessageHandlerOption func(*MessageHandler)

// WithPlaylistTriggers routes tracks to playlists based on triggers in the message
func WithPlaylistTriggers(triggers *trigger.Config) MessageHandlerOption {
	return func(h *MessageHandler) {
		h.triggers = triggers
	}
}

// WithTrackRouter routes tracks in messages without a known playlist trigger through
// the given router (e.g. genre-based) instead of adding them to the default playlist
func WithTrackRouter(router TrackRouter) MessageHandlerOption {
	return func(h *MessageHandler) {
		h.trackRouter = router
	}
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(playlistAdder PlaylistAdder, actions ChannelActions, opts ...MessageHandlerOption) *MessageHandler {
	h := &MessageHandler{playlistAdder: playlistAdder, actionIDs: actions}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
			addTracksToPlaylist := &AddTracksToPlaylist{session: s, event: m, playlistAdder: h.playlistAdder, triggers: h.triggers, trackRouter: h.trackRouter}
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
	session       *discordgo.Session
	event         *discordgo.MessageCreate
	playlistAdder PlaylistAdder
	triggers      *trigger.Config
	trackRouter   TrackRouter
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

	// Known triggers pick the playlists; otherwise the track router decides, when configured
	if !a.hasKnownTrigger(ctx) && a.trackRouter != nil {
		if err := a.trackRouter.RouteTracksToPlaylist(ctx, a.event.Author.ID, trackURLs); err != nil {
			logger.With(zap.Error(err)).Error("Failed to route tracks to playlist", fields...)
		}
		return
	}

	playlistIDs := a.playlistIDs(ctx)
	if len(playlistIDs) == 0 {
		return
	}

	ctx, fields = ctxutil.WithZapFields(
		ctx,
		zap.Strings(zapkey.PlaylistIDs, playlistIDs),
	)

	if err := a.playlistAdder.AddTracksToPlaylist(ctx, a.event.Author.ID, trackURLs, playlistIDs...); err != nil {
		logger.With(zap.Error(err)).Error("Failed to add tracks to playlist", fields...)
		return
	}
}

// hasKnownTrigger reports whether the message uses at least one configured playlist trigger
// (including the reserved default trigger). Unknown triggers are logged (and optionally replied to).
func (a *AddTracksToPlaylist) hasKnownTrigger(ctx context.Context) bool {
	if a.triggers == nil {
		return false
	}
	names := a.triggers.Extract(a.event.Content)
	_, unknown := a.triggers.Resolve(a.event.Content)
	a.reportUnknownTriggers(ctx, unknown)
	return len(names) > len(unknown)
}

// playlistIDs returns the playlists to add the message's tracks to.
// Unknown triggers are ignored here; hasKnownTrigger reports them.
func (a *AddTracksToPlaylist) playlistIDs(ctx context.Context) []string {
	fields := ctxutil.ZapFields(ctx)

	// Without triggers configured, only the default playlist is used
	if a.triggers == nil {
		playlistID := os.Getenv(envvar.SpotifyPlaylistID)
		if playlistID == "" {
			logger.With(zap.Error(fmt.Errorf("failed to retrieve playlist ID from env var"))).Error("Playlist ID is empty", fields...)
			return nil
		}
		return []string{playlistID}
	}

	playlistIDs, _ := a.triggers.Resolve(a.event.Content)
	return playlistIDs
}

// reportUnknownTriggers logs (and optionally replies to) any unknown trigger names
func (a *AddTracksToPlaylist) reportUnknownTriggers(ctx context.Context, unknown []string) {
	if len(unknown) == 0 {
		return
	}
	logger.With(zap.Strings(zapkey.Triggers, unknown)).Warn("Unknown playlist triggers", ctxutil.ZapFields(ctx)...)
	if a.triggers.ReplyUnknown {
		a.replyUnknownTriggers(ctx, unknown)
	}
}

// replyUnknownTriggers replies to the message listing the unknown triggers
func (a *AddTracksToPlaylist) replyUnknownTriggers(ctx context.Context, unknown []string) {
	names := make([]string, len(unknown))
	for i, name := range unknown {
		names[i] = a.triggers.Prefix + name
	}
	reply := &Reply{
		session:  a.session,
		event:    a.event,
		response: "Unknown playlist: " + strings.Join(names, ", "),
	}
	reply.Execute(ctx)
}

// Validate validates the action
func (a *AddTracksToPlaylist) Validate() error {
	if a.playlistAdder == nil {
		return fmt.Errorf("playlist adder is nil")
	}
	if a.event == nil {
		return fmt.Errorf("message is nil")
	}
	if a.session == nil {
		return fmt.Errorf("session is nil")
	}
	return nil
}
