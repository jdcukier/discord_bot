package spotify

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jdcukier/spotify/v2"
	auth "github.com/jdcukier/spotify/v2/auth"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"discordbot/constants/zapkey"
	"discordbot/spotify/config"
)

// MessageSender is an interface for posting messages
// This will primarily be used for posting the Spotify Auth link to the user instead of
// needing to check the logs to find it.
// Note: This may be expanded to support other message posting in the future.
type MessageSender interface {
	SendMessage(ctx context.Context, channelType string, message string) error
}

// Client represents a spotify client
type Client struct {
	// Clients
	api       *spotify.Client
	messenger MessageSender

	// Configuration
	config *config.Config

	// Authentication
	authenticator *auth.Authenticator
	tokenChan     chan *oauth2.Token
	token         *oauth2.Token
}

// NewClient initializes Spotify client using Authorization Code Flow.
func NewClient(opts ...Option) (*Client, error) {
	// Initialize client
	c := &Client{
		tokenChan: make(chan *oauth2.Token, 1),
	}

	// Apply options to override defaults
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// If a config wasn't provided, create the default config
	if c.config == nil {
		config, err := config.NewConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create config: %w", err)
		}
		c.config = config
	}

	// Initialize authenticator
	c.authenticator = auth.New(
		auth.WithRedirectURL(c.config.RedirectURI),
		auth.WithScopes(
			auth.ScopePlaylistModifyPublic,
			auth.ScopePlaylistModifyPrivate,
		),
		auth.WithClientID(c.config.ClientID),
		auth.WithClientSecret(c.config.ClientSecret),
	)

	return c, nil
}

// String returns a string representation of the client
func (c *Client) String() string {
	return "Spotify Client"
}

// -- Start/Stop ---

func (c *Client) Start() error {
	// Register handlers
	http.HandleFunc("/spotify/callback", c.callbackHandler())

	// Try to load existing token first
	if err := c.loadToken(); err != nil {
		logger.Error("Failed to load token", zap.Error(err))
		return fmt.Errorf("failed to load spotify token: %w", err)
	}

	// If no token exists, start auth flow
	if c.token == nil {
		logger.Info("No existing token found, starting authentication flow")
		token, err := c.authenticate()
		if err != nil {
			return fmt.Errorf("failed to authenticate spotify client: %w", err)
		}
		c.token = token
		if err := c.saveToken(); err != nil {
			logger.Error("Failed to save token", zap.Error(err))
			return fmt.Errorf("failed to save spotify token: %w", err)
		}
	}

	// Log the scopes in the token
	logger.Info("Spotify token loaded", zap.Any(zapkey.Scopes, c.token.Extra("scope")))

	// Initialize API
	httpClient := c.authenticator.Client(context.Background(), c.token)
	c.api = spotify.New(httpClient)

	// Test authentication by getting current user
	user, err := c.api.CurrentUser(context.Background())
	if err != nil {
		logger.Error("Spotify authentication test failed", zap.Error(err))
		return fmt.Errorf("spotify authentication failed: %w", err)
	}
	logger.Info("Spotify authentication successful", zap.String(zapkey.UserName, user.DisplayName), zap.String(zapkey.UserID, user.ID))

	return nil
}

func (c *Client) Stop() error {
	return nil
}
