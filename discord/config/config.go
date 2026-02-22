// Package config provides utilities for managing Discord configuration
package config

import (
	"fmt"
	"os"

	"discordbot/constants/envvar"
)

// Config represents the configuration for the Discord client
type Config struct {
	Token         string
	AuthChannelID string
}

// NewConfig creates a new configuration struct for the Discord client
func NewConfig(opts ...Option) (*Config, error) {
	c := &Config{
		Token:         os.Getenv(envvar.DiscordToken),
		AuthChannelID: os.Getenv(envvar.DiscordAuthChannelID),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("discord token is not set")
	}
	if c.AuthChannelID == "" {
		// Authentication channel is not required, but it is recommended for ease of use
		logger.Warn("authentication channel ID is not set")
	}
	return nil
}

// Option is a function that overrides a default configuration value
type Option func(*Config)

// WithAuthChannelID sets the authentication channel ID
func WithAuthChannelID(channelID string) Option {
	return func(c *Config) {
		c.AuthChannelID = channelID
	}
}

// WithToken sets the discord token
func WithToken(token string) Option {
	return func(c *Config) {
		c.Token = token
	}
}
