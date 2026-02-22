// Package config provides utilities for managing Spotify configuration
package config

import (
	"fmt"
	"os"
	"strings"

	"discordbot/constants/envvar"
)

// Config represents the configuration for the Spotify client
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// NewConfig creates a new configuration struct for the Spotify client
func NewConfig(opts ...Option) (*Config, error) {
	c := &Config{
		ClientID:     os.Getenv(envvar.SpotifyAppID),
		ClientSecret: os.Getenv(envvar.SpotifySecret),
		RedirectURI:  os.Getenv(envvar.SpotifyRedirectURI),
	}
	for _, opt := range opts {
		opt(c)
	}

	// Validate config
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate all configuration
func (c *Config) Validate() error {
	var missing []string
	if c.ClientID == "" {
		missing = append(missing, "Client ID")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "Client Secret")
	}
	if c.RedirectURI == "" {
		missing = append(missing, "Redirect URI")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Option is a function that overrides a default configuration value
type Option func(*Config)

// WithClientID overrides the default Client ID
func WithClientID(clientID string) Option {
	return func(c *Config) {
		c.ClientID = clientID
	}
}

// WithClientSecret overrides the default Client Secret
func WithClientSecret(clientSecret string) Option {
	return func(c *Config) {
		c.ClientSecret = clientSecret
	}
}

// WithRedirectURI overrides the default Redirect URI
func WithRedirectURI(redirectURI string) Option {
	return func(c *Config) {
		c.RedirectURI = redirectURI
	}
}
