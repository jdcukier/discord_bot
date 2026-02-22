package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/discord/config"
)

// -- Client --

type Handler interface {
	fmt.Stringer
	Add(session *discordgo.Session) error
}

// Client represents a discord client
type Client struct {
	session  *discordgo.Session
	config   *config.Config
	handlers []Handler
}

// NewClient creates a new discord client
func NewClient(options ...Option) (*Client, error) {
	// Initialize client
	c := &Client{}

	// Apply options to override defaults
	for _, opt := range options {
		opt(c)
	}

	// If a config wasn't provided, create the default config
	if c.config == nil {
		config, err := config.NewConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create config: %w", err)
		}
		c.config = config
	}

	// If a session wasn't provided, create a new one
	if c.session == nil {
		if err := c.createSession(); err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
	}

	// Register discord event handlers
	if err := c.registerHandlers(); err != nil {
		return nil, fmt.Errorf("failed to register handlers: %w", err)
	}

	return c, nil
}

// createSession creates a new discord session
func (c *Client) createSession() error {
	if c.session != nil {
		logger.Warn("overriding existing discord session")
	}
	session, err := discordgo.New("Bot " + c.config.Token)
	if err != nil {
		return fmt.Errorf("failed to create discord session: %w", err)
	}
	c.session = session

	return nil
}

// registerHandlers sets callbacks for the discord session to handle registered events
func (c *Client) registerHandlers() error {
	if c.session == nil {
		return fmt.Errorf("discord session is nil")
	}
	if len(c.handlers) == 0 {
		return fmt.Errorf("no discord handlers to register")
	}

	// Register handlers
	for _, handler := range c.handlers {
		logger.Info("adding handler", zap.Stringer("handler", handler))
		if err := handler.Add(c.session); err != nil {
			return fmt.Errorf("failed to add %q handler: %w", handler, err)
		}
	}

	return nil
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
