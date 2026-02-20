package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zmb3/spotify/v2"
	auth "github.com/zmb3/spotify/v2/auth"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/log"
	"discordbot/spotify/track"
	"discordbot/utils/ctxutil"
)

const (
	state     = "discord-bot-state" // TODO: Make this randomized
	tokenFile = "spotify_token.json"
)

// Client represents a spotify client
type Client struct {
	api           *spotify.Client
	authenticator *auth.Authenticator
	tokenChan     chan *oauth2.Token
	token         *oauth2.Token
}

// NewClient initializes Spotify client using Authorization Code Flow.
func NewClient() (*Client, error) {
	c := &Client{
		tokenChan: make(chan *oauth2.Token, 1),
	}

	// Grab env vars
	clientID := os.Getenv(envvar.SpotifyID)
	clientSecret := os.Getenv(envvar.SpotifySecret)
	redirectURI := os.Getenv(envvar.SpotifyRedirectURI)

	// Check for missing env vars
	var missing []string
	if clientID == "" {
		missing = append(missing, envvar.SpotifyID)
	}
	if clientSecret == "" {
		missing = append(missing, envvar.SpotifySecret)
	}

	if redirectURI == "" {
		missing = append(missing, envvar.SpotifyRedirectURI)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing env vars: %s", strings.Join(missing, ", "))
	}

	// Initialize authenticator
	c.authenticator = auth.New(
		auth.WithRedirectURL(redirectURI),
		auth.WithScopes(
			auth.ScopePlaylistModifyPublic,
			auth.ScopePlaylistModifyPrivate,
		),
		auth.WithClientID(clientID),
		auth.WithClientSecret(clientSecret),
	)

	return c, nil
}

// String returns a string representation of the client
func (c *Client) String() string {
	return "Spotify Client"
}

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

// refreshTokenIfNeeded checks and refreshes token if expired
func (c *Client) refreshTokenIfNeeded(ctx context.Context) error {
	if c.token != nil && c.token.Valid() {
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

// -- Start/Stop ---

func (c *Client) Start() error {
	// Register handlers
	http.HandleFunc("/spotify/callback", c.callbackHandler())

	// Try to load existing token first
	if err := c.loadToken(); err != nil {
		log.Logger.Error("Failed to load token", zap.Error(err))
		return fmt.Errorf("failed to load spotify token: %w", err)
	}

	// If no token exists, start auth flow
	if c.token == nil {
		log.Logger.Info("No existing token found, starting authentication flow")
		token, err := c.authenticate()
		if err != nil {
			return fmt.Errorf("failed to authenticate spotify client: %w", err)
		}
		c.token = token
		if err := c.saveToken(); err != nil {
			log.Logger.Error("Failed to save token", zap.Error(err))
			return fmt.Errorf("failed to save spotify token: %w", err)
		}
	}

	// Log the scopes in the token
	log.Logger.Info("Spotify token loaded", zap.Any(zapkey.Scopes, c.token.Extra("scope")))

	// Initialize API
	httpClient := c.authenticator.Client(context.Background(), c.token)
	c.api = spotify.New(httpClient)

	// Test authentication by getting current user
	user, err := c.api.CurrentUser(context.Background())
	if err != nil {
		log.Logger.Error("Spotify authentication test failed", zap.Error(err))
		return fmt.Errorf("spotify authentication failed: %w", err)
	}
	log.Logger.Info("Spotify authentication successful", zap.String(zapkey.UserName, user.DisplayName), zap.String(zapkey.UserID, user.ID))

	return nil
}

func (c *Client) Stop() error {
	return nil
}

// -- Tracks ---

// AddTracksToPlaylist adds tracks to a playlist from a list of track URLs
// TODO: Paginate if necessary - Spotify API has a limit of 100 tracks per request
func (c *Client) AddTracksToPlaylist(
	ctx context.Context,
	playlistID string,
	trackURLs []string,
) error {
	ctx, fields := ctxutil.WithZapFields(
		ctx,
		zap.String(zapkey.PlaylistID, playlistID),
		zap.Strings(zapkey.TrackURLs, trackURLs),
	)

	if c.token == nil {
		logger.Error("Spotify token is nil", fields...)
		return fmt.Errorf("spotify token is nil")
	}

	currentUser, err := c.api.CurrentUser(ctx)
	if err != nil {
		logger.With(zap.Error(err)).Error("Spotify authentication test failed", fields...)
		return fmt.Errorf("spotify authentication failed: %w", err)
	}

	ctx, fields = ctxutil.WithZapFields(
		ctx,
		zap.Any(zapkey.Scopes, c.token.Extra("scope")),
		zap.String(zapkey.UserName, currentUser.DisplayName),
		zap.String(zapkey.UserID, currentUser.ID),
	)

	// Auto-refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		log.Logger.With(zap.Error(err)).Error("Failed to refresh token", fields...)
		return fmt.Errorf("failed to refresh spotify token: %w", err)
	}

	verboseLogsEnabled, err := strconv.ParseBool(os.Getenv(envvar.VerboseLogsEnabled))
	if err != nil {
		log.Logger.With(zap.Error(err)).Warn("Failed to parse verbose logs enabled", fields...)
	}
	if verboseLogsEnabled {
		playlist, err := c.api.GetPlaylist(ctx, spotify.ID(playlistID))
		if err != nil {
			log.Logger.With(zap.Error(err)).Error("Cannot access playlist", fields...)
			return fmt.Errorf("cannot access playlist %s: %w", playlistID, err)
		}
		ctx, fields = ctxutil.WithZapFields(
			ctx,
			zap.Any(zapkey.Data, playlist),
			zap.String(zapkey.PlaylistOwnerID, playlist.Owner.ID),
		)

		log.Logger.Info("Playlist access verified", fields...)
	}

	// Convert track URLs to track IDs
	trackIDs := track.ToTrackIDs(trackURLs)

	ctx, fields = ctxutil.WithZapFields(
		ctx,
		zap.String(zapkey.PlaylistID, playlistID),
		zap.Strings(zapkey.TrackURLs, trackURLs),
		zap.Any(zapkey.TrackIDs, trackIDs),
	)

	// Determine tracks that are already in the playlist to avoid duplicates
	playlistItemPage, err := c.api.GetPlaylistItems(ctx, spotify.ID(playlistID))
	if err != nil {
		logger.With(zap.Error(err)).Error("Cannot access playlist tracks", fields...)
		return fmt.Errorf("cannot access playlist tracks %s: %w", playlistID, err)
	}
	filteredTrackIDs := track.FilterTracks(playlistItemPage, trackIDs)

	// If no new tracks, return early
	if len(filteredTrackIDs) == 0 {
		logger.Info("No new tracks to add", fields...)
		return nil
	}
	if verboseLogsEnabled {
		logger.With(zap.Any(zapkey.TrackIDs, filteredTrackIDs)).Info("Filtered track IDs", fields...)
	}

	// Add tracks to playlist
	snapshotID, err := c.api.AddTracksToPlaylist(
		ctx,
		spotify.ID(playlistID),
		filteredTrackIDs...,
	)

	// Log detailed error information
	if err != nil {
		logger.With(zap.Error(err), zap.String(zapkey.Data, snapshotID)).Error("Spotify API error", fields...)
	}

	return err
}

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
