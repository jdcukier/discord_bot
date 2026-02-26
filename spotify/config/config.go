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
	WorkerURL            string // Base URL of the Cloudflare Worker
	CFAccessClientID     string // CF Access service token client ID
	CFAccessClientSecret string // CF Access service token client secret
}

// NewConfig creates a new configuration struct for the Spotify client
func NewConfig(opts ...Option) (*Config, error) {
	c := &Config{
		WorkerURL:            os.Getenv(envvar.SpotifyWorkerURL),
		CFAccessClientID:     os.Getenv(envvar.CFAccessClientID),
		CFAccessClientSecret: os.Getenv(envvar.CFAccessClientSecret),
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
	if c.WorkerURL == "" {
		missing = append(missing, "Cloudflare Worker URL")
	}
	if c.CFAccessClientID == "" {
		missing = append(missing, "CF Access Client ID")
	}
	if c.CFAccessClientSecret == "" {
		missing = append(missing, "CF Access Client Secret")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Option is a function that overrides a default configuration value
type Option func(*Config)

// WithWorkerURL overrides the default Cloudflare Worker base URL.
func WithWorkerURL(u string) Option {
	return func(c *Config) { c.WorkerURL = u }
}

// WithCFAccessClientID overrides the default Cloudflare Access service token client ID.
func WithCFAccessClientID(id string) Option {
	return func(c *Config) { c.CFAccessClientID = id }
}

// WithCFAccessClientSecret overrides the default Cloudflare Access service token client secret.
func WithCFAccessClientSecret(s string) Option {
	return func(c *Config) { c.CFAccessClientSecret = s }
}
