package spotify

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jdcukier/spotify/v2"

	"discordbot/constants/zapkey"
	"discordbot/utils/ctxutil"
)

// playlist gets metadata about the specified playlist from spotify.
// Note: This does not include the list of tracks. Use allPlaylistTrackIDs to get the full set of track IDs.
func (c *Client) playlist(ctx context.Context, api *spotify.Client, playlistID string) (*spotify.FullPlaylist, error) {
	if playlistID == "" {
		return nil, fmt.Errorf("no playlist ID provided")
	}
	return api.GetPlaylist(ctx, spotify.ID(playlistID))
}

// allPlaylistTrackIDs fetches all track IDs in a playlist, paginating through every page.
func (c *Client) allPlaylistTrackIDs(ctx context.Context, api *spotify.Client, playlistID string) (map[spotify.ID]struct{}, error) {
	if playlistID == "" {
		return nil, fmt.Errorf("no playlist ID provided")
	}
	existing := make(map[spotify.ID]struct{})
	page, err := api.GetPlaylistItems(ctx, spotify.ID(playlistID))
	for ; err == nil; err = api.NextPage(ctx, page) {
		for _, item := range page.Items {
			if item.Item.Track != nil {
				existing[item.Item.Track.ID] = struct{}{}
			}
		}
		if page.Next == "" {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return existing, nil
}

// logPlaylistInfo logs information about the specified playlist.
func (c *Client) logPlaylistInfo(ctx context.Context, api *spotify.Client, playlistID string) {
	// Logging metadata
	fields := ctxutil.ZapFields(ctx)

	// Get playlist
	pl, err := c.playlist(ctx, api, playlistID)
	if err != nil {
		logger.With(zap.Error(err)).Error("Cannot access playlist", fields...)
		return
	}

	// Log playlist info
	logger.With(
		zap.Any(zapkey.Data, pl),
		zap.String(zapkey.PlaylistOwnerID, pl.Owner.ID),
	).Info("Playlist access verified", fields...)
}
