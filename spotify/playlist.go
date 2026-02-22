package spotify

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jdcukier/spotify/v2"

	"discordbot/constants/zapkey"
	"discordbot/utils/ctxutil"
)

// Playlist gets metadata about the specified playlist from spotify.
// Note: This does not include the list of tracks. Use PlaylistItems to get the list of tracks.
func (c *Client) Playlist(ctx context.Context, playlistID string) (*spotify.FullPlaylist, error) {
	if c.api == nil {
		return nil, fmt.Errorf("spotify client not initialized")
	}
	if playlistID == "" {
		return nil, fmt.Errorf("no playlist ID provided")
	}
	return c.api.GetPlaylist(ctx, spotify.ID(playlistID))
}

// PlaylistItems gets a list of tracks saved in the specified playlist.
func (c *Client) PlaylistItems(ctx context.Context, playlistID string) (*spotify.PlaylistItemPage, error) {
	if c.api == nil {
		return nil, fmt.Errorf("spotify client not initialized")
	}
	if playlistID == "" {
		return nil, fmt.Errorf("no playlist ID provided")
	}
	return c.api.GetPlaylistItems(ctx, spotify.ID(playlistID))
}

// logPlaylistInfo logs information about the specified playlist.
func (c *Client) logPlaylistInfo(ctx context.Context, playlistID string) {
	// Logging metadata
	fields := ctxutil.ZapFields(ctx)

	// Get playlist
	playlist, err := c.Playlist(ctx, playlistID)
	if err != nil {
		logger.With(zap.Error(err)).Error("Cannot access playlist", fields...)
		return
	}

	// Log playlist info
	logger.With(
		zap.Any(zapkey.Data, playlist),
		zap.String(zapkey.PlaylistOwnerID, playlist.Owner.ID),
	).Info("Playlist access verified", fields...)
}
