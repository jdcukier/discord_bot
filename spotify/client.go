// Package spotify provides a client for interacting with the Spotify API
package spotify

import (
	"fmt"
	"net/http"

	spotifyapi "github.com/zmb3/spotify/v2"
)

type Config struct {
	ClientID     string
	ClientSecret string
}

func (c *Config) Refresh() error {
	// TODO: Implement config refresh logic
	return nil
}

func NewClient() (*spotifyapi.Client, error) {
	config := Config{}
	if err := config.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to refresh config: %w", err)
	}

	return spotifyapi.New(http.DefaultClient), nil
}
