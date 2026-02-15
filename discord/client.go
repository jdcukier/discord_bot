package discord

import (
	"fmt"
	"os"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
)

// -- Config --

type Config struct {
	Token string
}

// Refresh retrieves the latest configuration from the env
func (c *Config) Refresh() error {
	token := os.Getenv(envvar.DiscordToken)
	if token == "" {
		return fmt.Errorf("DISCORD_TOKEN environment variable is not set")
	}
	c.Token = token
	return nil
}

// -- Client --

type Handler interface {
	fmt.Stringer
	Add(session *discordgo.Session) error
}

// Client represents a Client client
type Client struct {
	session  *discordgo.Session
	config   Config
	handlers []Handler
}

// NewClient creates a new Client client with the given token
func NewClient() (*Client, error) {
	c := &Client{
		config: Config{},
	}
	if err := c.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to retrieve discord client config: %w", err)
	}

	session, err := discordgo.New("Bot " + c.config.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	// TODO: Make these configurable
	c.handlers = append(c.handlers, &MessageHandler{})

	for _, handler := range c.handlers {
		zap.L().Info("adding handler", zap.Stringer("handler", handler))
		if err := handler.Add(session); err != nil {
			return nil, fmt.Errorf("failed to add %q handler: %w", handler, err)
		}
	}

	return &Client{session: session}, nil
}

// ---- Start/Stop ----

func (c *Client) Start() error {
	return c.session.Open()
}

func (c *Client) Close() error {
	return c.session.Close()
}

// --- Config ---

func (c *Client) Refresh() error {
	if err := c.config.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh config: %w", err)
	}
	return nil
}
