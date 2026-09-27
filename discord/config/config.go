// Package config provides utilities for managing Discord configuration
package config

import (
	"errors"
	"fmt"
	"os"

	"discordbot/constants/envvar"
	"discordbot/discord/channel"
)

// ErrNotConfigured is returned by Load when no token is set for the namespace,
// so callers can skip optional bots instead of failing.
var ErrNotConfigured = errors.New("discord bot not configured")

// channelEnvVars maps each channel type to its (un-namespaced) env var
var channelEnvVars = map[channel.Type]string{
	channel.Auth:  envvar.DiscordAuthChannelID,
	channel.Debug: envvar.DiscordDebugChannelID,
	channel.Songs: envvar.DiscordSongsChannelID,
}

// Config represents the configuration for the Discord client
type Config struct {
	Namespace  string
	Token      string
	AppID      string
	ChannelIDs map[channel.Type]string

	// Optional; each bot applies its own defaults when empty
	ReadyMessage     string // Posted when the bot comes online
	ListeningMessage string // Shown as the bot's "Listening to" activity
}

// Load creates the configuration for the bot in the given namespace, reading
// namespaced env vars such as DISCORD_TOKEN_<NAMESPACE>.
// Returns ErrNotConfigured if the namespace has no token set.
func Load(namespace string, opts ...Option) (*Config, error) {
	c := &Config{
		Namespace:        namespace,
		Token:            os.Getenv(envvar.Namespaced(envvar.DiscordToken, namespace)),
		AppID:            os.Getenv(envvar.Namespaced(envvar.DiscordAppID, namespace)),
		ChannelIDs:       make(map[channel.Type]string),
		ReadyMessage:     os.Getenv(envvar.Namespaced(envvar.BotReadyMessage, namespace)),
		ListeningMessage: os.Getenv(envvar.Namespaced(envvar.BotListeningMessage, namespace)),
	}
	for channelType, key := range channelEnvVars {
		if id := os.Getenv(envvar.Namespaced(key, namespace)); id != "" {
			c.ChannelIDs[channelType] = id
		}
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.Token == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrNotConfigured, envvar.Namespaced(envvar.DiscordToken, namespace))
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid %s discord configuration: %w", namespace, err)
	}
	return c, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Required fields
	if c.Token == "" {
		return fmt.Errorf("discord token is not set")
	}
	if c.AppID == "" {
		return fmt.Errorf("discord app ID is not set")
	}

	// Optional fields
	if c.ChannelIDs == nil {
		c.ChannelIDs = make(map[channel.Type]string)
	}
	return nil
}

// RequireChannels returns an error if any of the given channel types has no ID set
func (c *Config) RequireChannels(channelTypes ...channel.Type) error {
	for _, channelType := range channelTypes {
		if c.ChannelIDs[channelType] == "" {
			return fmt.Errorf("%s channel ID is not set (%s)",
				channelType, envvar.Namespaced(channelEnvVars[channelType], c.Namespace))
		}
	}
	return nil
}

// Option is a function that overrides a default configuration value
type Option func(*Config)

// WithAuthChannelID sets the authentication channel ID
func WithAuthChannelID(channelType channel.Type, channelID string) Option {
	return func(c *Config) {
		if c.ChannelIDs == nil {
			c.ChannelIDs = make(map[channel.Type]string)
		}
		c.ChannelIDs[channelType] = channelID
	}
}

// WithToken sets the discord token
func WithToken(token string) Option {
	return func(c *Config) {
		c.Token = token
	}
}
