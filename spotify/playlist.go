package spotify

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jdcukier/spotify/v2"

	"discordbot/constants/zapkey"
	"discordbot/log"
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

// FetchPlaylistName looks up a playlist's name on Spotify with a Discord user's Spotify access.
// Unlike adding tracks, it never starts the auth flow or posts to Discord: names are cosmetic,
// so callers just keep the name they have on errors (e.g. worker.ErrAuthRequired).
func (c *Client) FetchPlaylistName(ctx context.Context, userID, playlistID string) (string, error) {
	if playlistID == "" {
		return "", fmt.Errorf("no playlist ID provided")
	}
	api := c.spotifyClientForUser(userID)
	pl, err := api.GetPlaylist(ctx, spotify.ID(playlistID), spotify.Fields("name"))
	if err != nil {
		return "", log.RedactErr(err, playlistID)
	}
	return pl.Name, nil
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
		logger.With(zap.Error(log.RedactErr(err, playlistID))).Error("Cannot access playlist", fields...)
		return
	}

	// Log playlist info. Not the whole playlist: it includes the ID, which must stay out of logs.
	logger.With(
		zap.String(zapkey.PlaylistOwnerID, pl.Owner.ID),
		zap.Bool(zapkey.Collaborative, pl.Collaborative),
		zap.Bool(zapkey.Public, pl.IsPublic),
	).Info("Playlist access verified", fields...)
}
