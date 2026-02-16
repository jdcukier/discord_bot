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

// Client represents a discord client
type Client struct {
	session *discordgo.Session
	config  Config
}

// NewClient creates a new discord client
func NewClient(handlers ...Handler) (*Client, error) {
	c := &Client{}
	if err := c.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to retrieve discord client config: %w", err)
	}

	session, err := discordgo.New("Bot " + c.config.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	for _, handler := range handlers {
		logger.Info("adding handler", zap.Stringer("handler", handler))
		if err := handler.Add(session); err != nil {
			return nil, fmt.Errorf("failed to add %q handler: %w", handler, err)
		}
	}

	return &Client{session: session}, nil
}

// String returns a string representation of the client
func (c *Client) String() string {
	return "Discord Client"
}

// ---- Start/Stop ----

// Start the discord client
func (c *Client) Start() error {
	err := c.session.Open()
	if err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}
	return nil
}

// Stop the discord client
func (c *Client) Stop() error {
	err := c.session.Close()
	if err != nil {
		logger.Error("failed to close discord session", zap.Error(err))
	}
	return nil
}

// --- Config ---

// Refresh the discord client config
func (c *Client) Refresh() error {
	if err := c.config.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh config: %w", err)
	}
	return nil
}
