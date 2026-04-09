package spotify

import (
	"discordbot/spotify/config"
	"discordbot/spotify/registry"
)

// Option is a function that configures a Client
type Option func(*Client) error

// WithMessenger configures the client with a component sender
func WithMessenger(messenger ComponentSender) Option {
	return func(c *Client) error {
		c.messenger = messenger
		return nil
	}
}

// WithConfig configures the client with a Spotify config
func WithConfig(cfg *config.Config) Option {
	return func(c *Client) error {
		c.config = cfg
		return nil
	}
}

// WithRegistry configures the client with a playlist genre registry
func WithRegistry(r *registry.Registry) Option {
	return func(c *Client) error {
		c.registry = r
		return nil
	}
}
