package spotify

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jdcukier/spotify/v2"
	auth "github.com/jdcukier/spotify/v2/auth"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/log"
	"discordbot/spotify/track"
	"discordbot/utils/ctxutil"
)

// MessagePoster is an interface for posting messages
// This will primarily be used for posting the Spotify Auth link to the user instead of
// needing to check the logs to find it.
// Note: This may be expanded to support other message posting in the future.
type MessagePoster interface {
	PostMessage(ctx context.Context, channelID string, message string) error
}

// Client represents a spotify client
type Client struct {
	// Clients
	api       *spotify.Client
	messenger MessagePoster

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
	if err := c.refreshToken(ctx); err != nil {
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
