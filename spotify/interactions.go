package spotify

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"discordbot/constants/zapkey"
)

// HandlePlaylistSelection is called when the user clicks a playlist button.
// selectedPlaylistID is either a Spotify playlist ID or "all" to add to all candidates.
func (c *Client) HandlePlaylistSelection(ctx context.Context, messageID string, selectedPlaylistID string) error {
	c.pendingMu.Lock()
	pending, ok := c.pendingInteractions[messageID]
	if ok {
		delete(c.pendingInteractions, messageID)
	}
	c.pendingMu.Unlock()

	if !ok {
		logger.Warn("No pending interaction found for message", zap.String(zapkey.ID, messageID))
		return nil
	}

	if selectedPlaylistID == "all" {
		for _, entry := range pending.candidates {
			if err := c.doAddTracks(ctx, pending.userID, entry.ID, pending.trackURLs); err != nil {
				logger.Error("Failed to add tracks to playlist", zap.Error(err), zap.String("playlist", entry.Name))
				c.reportToSongsChannel(ctx, fmt.Sprintf("❌ <@%s> Failed to add to **%s**: %v", pending.userID, entry.Name, err))
			} else {
				c.reportToSongsChannel(ctx, fmt.Sprintf("✅ <@%s> Added track to **%s**.", pending.userID, entry.Name))
			}
		}
		return nil
	}

	// Find the playlist name for a nicer confirmation message
	playlistName := selectedPlaylistID
	for _, entry := range pending.candidates {
		if entry.ID == selectedPlaylistID {
			playlistName = entry.Name
			break
		}
	}

	if err := c.doAddTracks(ctx, pending.userID, selectedPlaylistID, pending.trackURLs); err != nil {
		c.reportToSongsChannel(ctx, fmt.Sprintf("❌ <@%s> Failed to add to **%s**: %v", pending.userID, playlistName, err))
		return err
	}

	c.reportToSongsChannel(ctx, fmt.Sprintf("✅ <@%s> Added track to **%s**.", pending.userID, playlistName))
	return nil
}

// HandleNewPlaylistConfirm is called when the user submits a playlist name via the Discord modal.
// It creates the playlist, adds the pending tracks, and shares the playlist link.
func (c *Client) HandleNewPlaylistConfirm(ctx context.Context, messageID string, playlistName string) error {
	c.pendingMu.Lock()
	pending, ok := c.pendingInteractions[messageID]
	if ok {
		delete(c.pendingInteractions, messageID)
	}
	c.pendingMu.Unlock()

	if !ok {
		logger.Warn("No pending interaction found for message", zap.String(zapkey.ID, messageID))
		return nil
	}

	api := c.spotifyClientForUser(pending.userID)
	newPlaylist, err := api.CreatePlaylist(ctx, playlistName, "", true, false)
	if err != nil {
		c.reportToSongsChannel(ctx, fmt.Sprintf("❌ <@%s> Failed to create playlist **%s**: %v", pending.userID, playlistName, err))
		return fmt.Errorf("failed to create playlist: %w", err)
	}

	if err := c.doAddTracks(ctx, pending.userID, string(newPlaylist.ID), pending.trackURLs); err != nil {
		c.reportToSongsChannel(ctx, fmt.Sprintf("❌ <@%s> Created **%s** but failed to add track: %v", pending.userID, playlistName, err))
		return err
	}

	spotifyURL := newPlaylist.ExternalURLs["spotify"]
	c.reportToSongsChannel(ctx, fmt.Sprintf("✅ <@%s> Created playlist **%s** and added your track: %s", pending.userID, playlistName, spotifyURL))
	return nil
}

// HandleInteractionCancel is called when the user clicks Skip.
func (c *Client) HandleInteractionCancel(ctx context.Context, messageID string) {
	c.pendingMu.Lock()
	pending, ok := c.pendingInteractions[messageID]
	if ok {
		delete(c.pendingInteractions, messageID)
	}
	c.pendingMu.Unlock()

	if !ok {
		return
	}

	c.reportToSongsChannel(ctx, fmt.Sprintf("👍 <@%s> Skipped. Track not added.", pending.userID))
}
