package spotify

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	state = "discord-bot-state" // TODO: Make this randomized
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

// -- Start/Stop ---

func (c *Client) Start() error {
	// Register handlers
	http.HandleFunc("/spotify/callback", c.callbackHandler())

	// Load token
	if c.token == nil {
		// No token saved yet — start auth flow
		token, err := c.authenticate()
		if err != nil {
			return fmt.Errorf("failed to authenticate spotify client: %w", err)
		}
		c.token = token
	}

	// Log the scopes in the token
	logger.Info("Spotify token scopes", zap.Any(zapkey.Scopes, c.token.Extra("scope")))

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

	// Log full message data if verbose logs are enabled
	verboseLogsEnabled := log.VerboseLogsEnabled(ctx)
	if verboseLogsEnabled {
		// Get the current playlist info
		playlist, err := c.api.GetPlaylist(ctx, spotify.ID(playlistID))
		if err != nil {
			logger.With(zap.Error(err)).Error("Cannot access playlist", fields...)
			return fmt.Errorf("cannot access playlist %s: %w", playlistID, err)
		}
		ctx, fields = ctxutil.WithZapFields(
			ctx,
			zap.Any(zapkey.Data, playlist),
			zap.String(zapkey.PlaylistOwnerID, playlist.Owner.ID),
		)

		logger.Info("Playlist access verified", fields...)
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
