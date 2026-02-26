package spotify

import (
	"context"
	"errors"
	"fmt"

	"github.com/jdcukier/spotify/v2"
	"go.uber.org/zap"

	"discordbot/constants/zapkey"
	"discordbot/log"
	"discordbot/spotify/track"
	"discordbot/spotify/worker"
	"discordbot/utils/ctxutil"
)

// -- Tracks ---

// AddTracksToPlaylist adds tracks to a playlist from a list of track URLs.
// If the user has no Spotify token, auth is triggered and the track is retried automatically.
func (c *Client) AddTracksToPlaylist(ctx context.Context, userID, playlistID string, trackURLs []string) error {
	err := c.doAddTracks(ctx, userID, playlistID, trackURLs)

	if err == nil {
		return nil
	}

	if errors.Is(err, worker.ErrAuthRequired) {
		c.handleAuthRequired(ctx, userID, playlistID, trackURLs)
		return nil // Return nil because we've handled/queued the retry
	}

	c.handleSpotifyError(ctx, err, "add-tracks", userID)
	return err
}

// handleAuthRequired notifies the user that Spotify auth is needed, then queues
// the track-add operation to be retried automatically after auth completes.
func (c *Client) handleAuthRequired(ctx context.Context, userID, playlistID string, trackURLs []string) {
	c.reportToDiscord(ctx, fmt.Sprintf("⚠️ <@%s> Spotify auth needed...", userID))

	c.triggerAuthIfNeeded(ctx, userID, func(authCtx context.Context) {
		if retryErr := c.doAddTracks(authCtx, userID, playlistID, trackURLs); retryErr != nil {
			c.reportToDiscord(authCtx, fmt.Sprintf("❌ <@%s> Could not add track: %v", userID, retryErr))
			return
		}
		c.reportToDiscord(authCtx, fmt.Sprintf("✅ <@%s> Your track was added!", userID))
	})
}

// doAddTracks performs the raw Spotify API calls to add tracks. No auth retry logic.
func (c *Client) doAddTracks(
	ctx context.Context,
	userID string,
	playlistID string,
	trackURLs []string,
) error {
	api := c.spotifyClientForUser(userID)

	ctx, fields := ctxutil.WithZapFields(
		ctx,
		zap.String(zapkey.PlaylistID, playlistID),
		zap.Strings(zapkey.TrackURLs, trackURLs),
		zap.String(zapkey.UserID, userID),
	)

	// Get the current user info for logging
	currentUser, err := c.currentUser(ctx, api)
	if err != nil {
		logger.With(zap.Error(err)).Error("Spotify authentication test failed", fields...)
		return fmt.Errorf("spotify authentication failed: %w", err)
	}

	ctx, fields = ctxutil.WithZapFields(
		ctx,
		zap.String(zapkey.UserName, currentUser.DisplayName),
	)

	verboseLogsEnabled := log.VerboseLogsEnabled(ctx)
	if verboseLogsEnabled {
		logger.Info("Checking playlist info", fields...)
		c.logPlaylistInfo(ctx, api, playlistID)
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
	existingTrackIDs, err := c.allPlaylistTrackIDs(ctx, api, playlistID)
	if err != nil {
		logger.With(zap.Error(err)).Error("Cannot access playlist tracks", fields...)
		return fmt.Errorf("cannot access playlist tracks %s: %w", playlistID, err)
	}
	filteredTrackIDs := track.FilterTracks(existingTrackIDs, trackIDs)

	// If no new tracks, return early
	if len(filteredTrackIDs) == 0 {
		logger.Info("No new tracks to add", fields...)
		return nil
	}
	if verboseLogsEnabled {
		logger.With(zap.Any(zapkey.TrackIDs, filteredTrackIDs)).Info("Filtered track IDs", fields...)
	}

	// Add tracks to playlist
	snapshotID, err := api.AddTracksToPlaylist(
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
