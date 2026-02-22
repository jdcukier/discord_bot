package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jdcukier/spotify/v2"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"discordbot/constants/zapkey"
	"discordbot/log"
)

const (
	state     = "discord-bot-state" // TODO: Make this randomized
	tokenFile = "spotify_token.json"
)

// --- Auth Flow ---

// Authenticate using Authorization Code Flow
func (c *Client) authenticate() (*oauth2.Token, error) {
	authURL := c.authenticator.AuthURL(state)
	fmt.Println("Open this URL in your browser to authenticate:")
	fmt.Println(authURL)

	// Wait for callback
	select {
	case token := <-c.tokenChan:
		logger.Info("Token received by auth flow", zap.Any(zapkey.Scopes, token.Extra("scope")), zap.Any(zapkey.Data, token))
		return token, nil
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timeout")
	}
}

// callbackHandler returns the HTTP handler for the Spotify OAuth callback
func (c *Client) callbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("state") != state {
			http.Error(w, "State mismatch", http.StatusForbidden)
			return
		}

		token, err := c.authenticator.Token(r.Context(), state, r)
		if err != nil {
			http.Error(w, "Couldn't get token", http.StatusForbidden)
			return
		}
		logger.Info("Callback received token", zap.Any(zapkey.Scopes, token.Extra("scope")), zap.Any(zapkey.Data, token))

		select {
		case c.tokenChan <- token:
		default:
			// Channel is full or closed, ignore
		}

		if _, err := fmt.Fprintln(w, "Spotify authentication successful! You can close this window."); err != nil {
			logger.Error("Failed to write response", zap.Error(err))
		}
	}
}

// --- Token Management ---

// saveToken saves the OAuth token to file
func (c *Client) saveToken() error {
	if c.token == nil {
		return fmt.Errorf("no token to save")
	}

	data, err := json.Marshal(c.token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenPath := filepath.Join(homeDir, ".discordbot", tokenFile)
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0755); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	if err := os.WriteFile(tokenPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	log.Logger.Info("Token saved to file", zap.String("path", tokenPath))
	return nil
}

// loadToken loads the OAuth token from file
func (c *Client) loadToken() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenPath := filepath.Join(homeDir, ".discordbot", tokenFile)
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Logger.Info("No existing token file found", zap.String("path", tokenPath))
			return nil // Not an error, just no token exists
		}
		return fmt.Errorf("failed to read token file: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("failed to unmarshal token: %w", err)
	}

	c.token = &token
	log.Logger.Info("Token loaded from file", zap.String("path", tokenPath))
	return nil
}

// isTokenValid checks if the current token is valid
func (c *Client) isTokenValid() bool {
	return c.token != nil && c.token.Valid()
}

// refreshToken checks and refreshes token if expired
func (c *Client) refreshToken(ctx context.Context) error {
	if c.isTokenValid() {
		return nil // Token is still valid
	}

	// Token is nil or expired, try to refresh
	log.Logger.Info("Token expired or missing, attempting refresh")

	if c.token == nil {
		// Try to load from file
		if err := c.loadToken(); err != nil {
			return fmt.Errorf("failed to load token: %w", err)
		}
	}

	if c.token != nil && !c.token.Valid() {
		// Try to refresh using refresh token
		newToken, err := c.authenticator.Exchange(ctx, c.token.RefreshToken)
		if err != nil {
			log.Logger.Error("Failed to refresh token", zap.Error(err))
			return fmt.Errorf("failed to refresh token: %w", err)
		}
		c.token = newToken
		if err := c.saveToken(); err != nil {
			log.Logger.Error("Failed to save refreshed token", zap.Error(err))
		}
	}

	// Update the API client with new token
	httpClient := c.authenticator.Client(ctx, c.token)
	c.api = spotify.New(httpClient)

	return nil
}
