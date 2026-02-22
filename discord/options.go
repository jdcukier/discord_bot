package discord

import (
	"github.com/bwmarrin/discordgo"

	"discordbot/discord/config"
)

// Option is a function that overrides a default client value
type Option func(*Client)

func WithConfig(config *config.Config) Option {
	return func(c *Client) {
		c.config = config
	}
}

func WithHandlers(handlers ...Handler) Option {
	return func(c *Client) {
		for _, handler := range handlers {
			if handler == nil {
				logger.Warn("nil handler provided")
				continue
			}
			c.handlers = append(c.handlers, handlers...)
		}
	}
}

func WithSession(session *discordgo.Session) Option {
	return func(c *Client) {
		c.session = session
	}
}
