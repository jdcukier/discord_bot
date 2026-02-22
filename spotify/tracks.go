package spotify

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jdcukier/spotify/v2"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/spotify/track"
	"discordbot/utils/ctxutil"
)

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

	// Refresh token if necessary
	if err := c.refreshToken(ctx); err != nil {
		logger.With(zap.Error(err)).Error("Failed to refresh token", fields...)
		return fmt.Errorf("failed to refresh spotify token: %w", err)
	}
	ctx, fields = ctxutil.WithZapFields(ctx, zap.Any(zapkey.Scopes, c.token.Extra("scope")))

	// Get the current user info for logging
	currentUser, err := c.CurrentUser(ctx)
	if err != nil {
		logger.With(zap.Error(err)).Error("Spotify authentication test failed", fields...)
		return fmt.Errorf("spotify authentication failed: %w", err)
	}

	ctx, fields = ctxutil.WithZapFields(
		ctx,
		zap.String(zapkey.UserName, currentUser.DisplayName),
		zap.String(zapkey.UserID, currentUser.ID),
	)

	verboseLogsEnabled, err := strconv.ParseBool(os.Getenv(envvar.VerboseLogsEnabled))
	if err != nil {
		logger.With(zap.Error(err)).Warn("Failed to parse verbose logs enabled", fields...)
	}
	if verboseLogsEnabled {
		c.logPlaylistInfo(ctx, playlistID)
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
	playlistItemPage, err := c.PlaylistItems(ctx, playlistID)
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
