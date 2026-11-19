package discord

import (
	"context"
	"fmt"
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
	appID string   // Application to register slash commands for
	tags  TagStore // Optional; enables the tag commands

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

// tagsCommand handles /tags, splitting long lists over several messages
func (h *InteractionSessionHandler) tagsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	chunks := listTags(h.tags.Config())
	respond(s, i, tagReply{content: chunks[0], private: true})
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

// tagCommand handles /tag set and /tag remove
func (h *InteractionSessionHandler) tagCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}
	sub := data.Options[0]
	var name, playlist string
	if opt := sub.GetOption(tagOption); opt != nil {
		name = opt.StringValue()
	}
	if opt := sub.GetOption(playlistOption); opt != nil {
		playlist = opt.StringValue()
	}

	var reply tagReply
	switch sub.Name {
	case tagSetSubcommand:
		reply = setTag(h.tags, name, playlist)
	case tagRemoveSubcommand:
		reply = removeTag(h.tags, name)
	default:
		logger.Error("unknown tag subcommand", zap.String(zapkey.Command, sub.Name))
		return
	}
	logger.Info("Handled tag command",
		zap.String(zapkey.Command, sub.Name),
		zap.String(zapkey.Name, name),
		zap.String(zapkey.UserID, interactionUserID(i)),
		zap.Bool(zapkey.Result, !reply.private)) // Replies contain playlist IDs, so don't log them
	respond(s, i, reply)
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
			choices = autocompleteTags(h.tags.Config(), opt.StringValue())
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

// respond replies to a slash command without pinging anyone
func respond(s *discordgo.Session, i *discordgo.InteractionCreate, reply tagReply) {
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
