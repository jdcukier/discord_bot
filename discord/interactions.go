package discord

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/id"
	"discordbot/constants/zapkey"
	"discordbot/utils/ctxutil"
	"discordbot/utils/stringutil"
)

// InteractionSessionHandler handles interactions
type InteractionSessionHandler struct {
	appID string         // Application to register slash commands for
	tags  TagStore       // Optional; enables the tag commands
	names *PlaylistNames // Optional; looks up the playlist names the tag commands show

	registered atomic.Bool // Whether the tag commands were registered by this process
}

// InteractionOption overrides a default interaction handler value
type InteractionOption func(*InteractionSessionHandler)

// WithTagCommands registers and handles the /tags and /tag commands for the application
func WithTagCommands(appID string, tags TagStore) InteractionOption {
	return func(h *InteractionSessionHandler) {
		h.appID = appID
		h.tags = tags
	}
}

// WithTagPlaylistNames looks up and saves the names /tags and the /tag replies show, if they aren't
// saved yet, with the Spotify access of the user who ran the command
func WithTagPlaylistNames(names *PlaylistNames) InteractionOption {
	return func(h *InteractionSessionHandler) {
		h.names = names
	}
}

// NewInteractionSessionHandler creates a new interaction session handler
func NewInteractionSessionHandler(opts ...InteractionOption) *InteractionSessionHandler {
	h := &InteractionSessionHandler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
	if h.tags != nil {
		if h.appID == "" {
			return fmt.Errorf("app ID is required for the tag commands")
		}
		// Register once per process, so the definitions stay in sync with the code without
		// using up Discord's daily limit on command creation when the gateway reconnects.
		// A failed registration is retried on the next READY.
		session.AddHandler(func(s *discordgo.Session, _ *discordgo.Ready) {
			if h.registered.Load() {
				return
			}
			if registerTagCommands(s, h.appID) {
				h.registered.Store(true)
			}
		})
	}
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
	case discordgo.InteractionApplicationCommandAutocomplete:
		h.autocomplete(s, i)
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
	case tagsCommand, tagCommand:
		if h.tags == nil {
			respond(s, i, tagReply{content: "Playlist tags aren't available right now.", private: true})
			return
		}
		if data.Name == tagsCommand {
			h.tagsCommand(s, i)
		} else {
			h.tagCommand(s, i)
		}
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

// tagsCommand handles /tags, splitting long lists over several messages. Missing playlist names are
// looked up with the Spotify access of the user who ran it, after deferring the reply, since Discord
// drops interactions that aren't answered within 3 seconds.
func (h *InteractionSessionHandler) tagsCommand(s interactionResponder, i *discordgo.InteractionCreate) {
	c := h.tags.Config()
	ids := listedPlaylists(c)
	name := cachedPlaylistNames(h.tags)
	deferred := false
	if h.names != nil && slices.ContainsFunc(ids, func(id string) bool { return h.names.Saved(id) == "" }) {
		if !deferResponse(s, i, true) {
			return
		}
		deferred = true
		names := h.names.Names(context.Background(), interactionUserID(i), ids)
		name = func(id string) string { return names[id] }
	}
	sendTagList(s, i, listTags(c, name), deferred)
}

// sendTagList sends the /tags messages privately: the first as the reply (or as the deferred reply's
// content), and the others as follow-ups
func sendTagList(s interactionResponder, i *discordgo.InteractionCreate, chunks []string, deferred bool) {
	if deferred {
		if !editResponse(s, i, chunks[0]) {
			return
		}
	} else {
		respond(s, i, tagReply{content: chunks[0], private: true})
	}
	for _, chunk := range chunks[1:] {
		if _, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
			Content:         chunk,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}); err != nil {
			logger.Error("failed to send tag list", zap.Error(err))
			return
		}
	}
}

// tagCommand handles /tag add, /tag set, /tag remove and /tag alias
func (h *InteractionSessionHandler) tagCommand(s interactionResponder, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}
	sub := data.Options[0]
	var name, playlist, newTags string
	if opt := sub.GetOption(tagOption); opt != nil {
		name = opt.StringValue()
	}
	if opt := sub.GetOption(playlistOption); opt != nil {
		playlist = opt.StringValue()
	}
	if opt := sub.GetOption(newTagsOption); opt != nil {
		newTags = opt.StringValue()
	}

	// Tag changes are only saved to disk, so this is quick enough to do before replying
	var reply tagReply
	switch sub.Name {
	case tagAddSubcommand:
		reply = addTag(h.tags, name, playlist)
	case tagSetSubcommand:
		reply = setTag(h.tags, name, playlist)
	case tagRemoveSubcommand:
		reply = removeTag(h.tags, name)
	case tagAliasSubcommand:
		reply = aliasTag(h.tags, name, newTags)
	default:
		logger.Error("unknown tag subcommand", zap.String(zapkey.Command, sub.Name))
		return
	}
	logger.Info("Handled tag command",
		zap.String(zapkey.Command, sub.Name),
		zap.String(zapkey.Name, name),
		zap.String(zapkey.UserID, interactionUserID(i)),
		zap.Bool(zapkey.Result, !reply.private)) // Replies contain playlist IDs, so don't log them
	h.sendTagReply(s, i, reply)
}

// sendTagReply sends a tag command's reply. Private replies (errors and "already" replies) and
// replies whose playlist name is saved are sent right away. Otherwise the public reply is deferred,
// the playlist's name is looked up with the Spotify access of the user who ran the command, and the
// reply is filled in with it, or with the fallback label if the name is still unknown.
func (h *InteractionSessionHandler) sendTagReply(s interactionResponder, i *discordgo.InteractionCreate, reply tagReply) {
	if reply.private || reply.withName == nil || h.names == nil {
		respond(s, i, reply)
		return
	}
	if name := h.names.Saved(reply.playlistID); name != "" {
		reply.content = reply.withName(name)
		respond(s, i, reply)
		return
	}
	if !deferResponse(s, i, false) {
		return
	}
	editResponse(s, i, reply.withName(h.names.Name(context.Background(), interactionUserID(i), reply.playlistID)))
}

// autocomplete suggests options while a slash command is being typed
func (h *InteractionSessionHandler) autocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name != tagCommand || h.tags == nil || len(data.Options) == 0 {
		return
	}
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, opt := range data.Options[0].Options {
		if opt.Focused && opt.Name == tagOption {
			choices = autocompleteTags(h.tags.Config(), opt.StringValue(), data.Options[0].Name == tagAliasSubcommand)
		}
	}
	// Sent directly because InteractionResponseData omits an empty choices list,
	// and Discord requires the field even when there's nothing to suggest
	endpoint := discordgo.EndpointInteractionResponse(i.ID, i.Token)
	body := autocompleteResponse{Type: discordgo.InteractionApplicationCommandAutocompleteResult}
	body.Data.Choices = choices
	if _, err := s.RequestWithBucketID("POST", endpoint, body, endpoint); err != nil {
		logger.Error("failed to respond to autocomplete", zap.Error(err))
	}
}

// autocompleteResponse is an autocomplete reply that always includes its choices
type autocompleteResponse struct {
	Type discordgo.InteractionResponseType `json:"type"`
	Data struct {
		Choices []*discordgo.ApplicationCommandOptionChoice `json:"choices"`
	} `json:"data"`
}

// interactionResponder replies to interactions; *discordgo.Session implements it
type interactionResponder interface {
	InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse, options ...discordgo.RequestOption) error
	InteractionResponseEdit(interaction *discordgo.Interaction, newresp *discordgo.WebhookEdit, options ...discordgo.RequestOption) (*discordgo.Message, error)
	FollowupMessageCreate(interaction *discordgo.Interaction, wait bool, data *discordgo.WebhookParams, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

// deferResponse acknowledges a slash command so its reply can take longer than Discord's 3 seconds,
// showing that the bot is thinking. Whether the reply is private is set here and can't be changed
// later. It reports whether it succeeded.
func deferResponse(s interactionResponder, i *discordgo.InteractionCreate, private bool) bool {
	var flags discordgo.MessageFlags
	if private {
		flags = discordgo.MessageFlagsEphemeral
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: flags},
	}); err != nil {
		logger.Error("failed to defer slash command response", zap.Error(err))
		return false
	}
	return true
}

// editResponse fills in a deferred reply without pinging anyone, and reports whether it succeeded
func editResponse(s interactionResponder, i *discordgo.InteractionCreate, content string) bool {
	content = truncate(content, maxMessageLength)
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:         &content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		logger.Error("failed to send deferred slash command response", zap.Error(err))
		return false
	}
	return true
}

// respond replies to a slash command without pinging anyone
func respond(s interactionResponder, i *discordgo.InteractionCreate, reply tagReply) {
	var flags discordgo.MessageFlags
	if reply.private {
		flags = discordgo.MessageFlagsEphemeral
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         truncate(reply.content, maxMessageLength),
			Flags:           flags,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	}); err != nil {
		logger.Error("failed to respond to slash command", zap.Error(err))
	}
}

// interactionUserID returns the ID of the user who triggered the interaction
func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}
