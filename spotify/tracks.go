package spotify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/jdcukier/spotify/v2"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/discord/channel"
	"discordbot/log"
	"discordbot/spotify/registry"
	"discordbot/spotify/track"
	"discordbot/spotify/worker"
	"discordbot/utils/ctxutil"
)

// Button custom ID prefixes used by the interaction handler.
const (
	CustomIDPlaylistSelect    = "playlist_select"
	CustomIDPlaylistSelectAll = "playlist_select_all"
	CustomIDNewPlaylist       = "new_playlist"
	CustomIDSkipPlaylist      = "skip_playlist"
)

// -- Track Routing ---

// RouteTracksToPlaylist determines the correct playlist(s) for the given track URLs based on
// artist genres, then either adds directly (single match), prompts the user (multiple/no match),
// or falls back to the legacy single-playlist mode when no registry is configured.
// Auth errors are handled transparently: the operation is queued and retried after auth completes.
func (c *Client) RouteTracksToPlaylist(ctx context.Context, userID string, trackURLs []string) error {
	err := c.doRouteTracksToPlaylist(ctx, userID, trackURLs)
	if err == nil {
		return nil
	}

	if errors.Is(err, worker.ErrAuthRequired) {
		c.handleAuthRequiredForRouting(ctx, userID, trackURLs)
		return nil
	}

	c.handleSpotifyError(ctx, err, "route-tracks", userID)
	return err
}

// handleAuthRequiredForRouting notifies the user that Spotify auth is needed and queues
// the routing operation to be retried automatically after auth completes.
func (c *Client) handleAuthRequiredForRouting(ctx context.Context, userID string, trackURLs []string) {
	c.reportToDiscord(ctx, fmt.Sprintf("⚠️ <@%s> Spotify auth needed...", userID))

	c.triggerAuthIfNeeded(ctx, userID, func(authCtx context.Context) {
		if retryErr := c.doRouteTracksToPlaylist(authCtx, userID, trackURLs); retryErr != nil {
			c.reportToDiscord(authCtx, fmt.Sprintf("❌ <@%s> Could not route track: %v", userID, retryErr))
		}
	})
}

// doRouteTracksToPlaylist performs the routing logic without auth retry handling.
func (c *Client) doRouteTracksToPlaylist(ctx context.Context, userID string, trackURLs []string) error {
	// No registry: fall back to legacy single-playlist mode
	if c.registry == nil {
		playlistID := os.Getenv(envvar.SpotifyPlaylistID)
		if playlistID == "" {
			return fmt.Errorf("no playlist configured: set SPOTIFY_PLAYLIST_CONFIG or SPOTIFY_PLAYLIST_ID")
		}
		return c.doAddTracks(ctx, userID, playlistID, trackURLs)
	}

	trackIDs := track.ToTrackIDs(trackURLs)
	if len(trackIDs) == 0 {
		return nil
	}

	api := c.spotifyClientForUser(userID)
	trackName, artistName, genres, err := c.primaryArtistGenres(ctx, api, trackIDs[0])
	if err != nil {
		return fmt.Errorf("failed to fetch artist genres: %w", err)
	}

	matches := c.registry.Match(genres)

	switch len(matches) {
	case 1:
		if err := c.doAddTracks(ctx, userID, matches[0].ID, trackURLs); err != nil {
			return err
		}
		c.reportToSongsChannel(ctx, fmt.Sprintf("✅ <@%s> Added **%s** to **%s**.", userID, trackName, matches[0].Name))
	case 0:
		c.promptNewPlaylist(ctx, userID, trackURLs, trackName, artistName, genres)
	default:
		c.promptPlaylistSelection(ctx, userID, trackURLs, trackName, artistName, matches)
	}

	return nil
}

// promptPlaylistSelection sends a Discord message with buttons for the user to pick a playlist.
// The pending interaction is stored keyed by the sent message's ID, which the interaction
// handler retrieves from i.Message.ID when the user clicks a button.
func (c *Client) promptPlaylistSelection(ctx context.Context, userID string, trackURLs []string, trackName, artistName string, candidates []registry.PlaylistEntry) {
	if c.messenger == nil {
		return
	}

	var buttons []discordgo.Button
	for _, entry := range candidates {
		buttons = append(buttons, discordgo.Button{
			Label:    entry.Name,
			Style:    discordgo.PrimaryButton,
			CustomID: CustomIDPlaylistSelect + ":" + entry.ID,
		})
	}
	buttons = append(buttons, discordgo.Button{
		Label:    "Add to All",
		Style:    discordgo.SecondaryButton,
		CustomID: CustomIDPlaylistSelectAll,
	})

	msg := fmt.Sprintf("🎵 <@%s> — **%s** by **%s** matches multiple playlists. Where should it go?",
		userID, trackName, artistName)

	msgID, err := c.messenger.SendMessageWithButtons(ctx, channel.Songs.String(), msg, buttons)
	if err != nil {
		logger.Warn("Failed to send playlist selection prompt", zap.Error(err))
		return
	}

	c.pendingMu.Lock()
	c.pendingInteractions[msgID] = &pendingInteraction{
		userID:     userID,
		trackURLs:  trackURLs,
		candidates: candidates,
	}
	c.pendingMu.Unlock()
}

// promptNewPlaylist sends a Discord message prompting the user to create a new playlist.
func (c *Client) promptNewPlaylist(ctx context.Context, userID string, trackURLs []string, trackName, artistName string, genres []string) {
	if c.messenger == nil {
		return
	}

	genreDisplay := "unknown"
	if len(genres) > 0 {
		genreDisplay = strings.Join(genres, ", ")
	}

	msg := fmt.Sprintf("🎵 <@%s> — **%s** by **%s** (genres: %s) didn't match any playlist. Create a new one?",
		userID, trackName, artistName, genreDisplay)

	buttons := []discordgo.Button{
		{
			Label:    "Create Playlist",
			Style:    discordgo.SuccessButton,
			CustomID: CustomIDNewPlaylist,
		},
		{
			Label:    "Skip",
			Style:    discordgo.DangerButton,
			CustomID: CustomIDSkipPlaylist,
		},
	}

	msgID, err := c.messenger.SendMessageWithButtons(ctx, channel.Songs.String(), msg, buttons)
	if err != nil {
		logger.Warn("Failed to send new playlist prompt", zap.Error(err))
		return
	}

	c.pendingMu.Lock()
	c.pendingInteractions[msgID] = &pendingInteraction{
		userID:    userID,
		trackURLs: trackURLs,
	}
	c.pendingMu.Unlock()
}

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
