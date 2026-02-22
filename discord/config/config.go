// Package config provides utilities for managing Discord configuration
package config

import (
	"fmt"
	"os"

	"discordbot/constants/envvar"
	"discordbot/discord/channel"
)

// Config represents the configuration for the Discord client
type Config struct {
	Token      string
	ChannelIDs map[channel.Type]string
}

// NewConfig creates a new configuration struct for the Discord client
func NewConfig(opts ...Option) (*Config, error) {
	c := &Config{
		Token: os.Getenv(envvar.DiscordToken),
		ChannelIDs: map[channel.Type]string{
			channel.Auth:  os.Getenv(envvar.DiscordAuthChannelID),
			channel.Debug: os.Getenv(envvar.DiscordDebugChannelID),
			channel.Songs: os.Getenv(envvar.DiscordSongsChannelID),
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid discord configuration: %w", err)
	}
	return c, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Required fields
	if c.Token == "" {
		return fmt.Errorf("discord token is not set")
	}

	// Optional fields
	if c.ChannelIDs == nil {
		c.ChannelIDs = make(map[channel.Type]string)
	}
	if c.ChannelIDs[channel.Auth] == "" {
		// Authentication channel is not required, but it is recommended for ease of use
		logger.Warn("authentication channel ID is not set")
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
