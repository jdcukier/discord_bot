package discord

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"discordbot/constants/zapkey"
	"discordbot/log"
	"discordbot/spotify/trigger"
)

const (
	// playlistNameTimeout limits how long a name lookup may take
	playlistNameTimeout = 10 * time.Second

	// playlistNameRefreshInterval is how often adding a song refreshes its playlist's name
	playlistNameRefreshInterval = time.Hour
)

// PlaylistNameFetcher looks up a playlist's current name on Spotify, with a Discord user's Spotify access.
// It must never start the auth flow or post to Discord.
type PlaylistNameFetcher interface {
	FetchPlaylistName(ctx context.Context, userID, playlistID string) (string, error)
}

// PlaylistNameStore saves the names of tagged playlists
type PlaylistNameStore interface {
	Config() *trigger.Config
	SetPlaylistName(id, name string) error
}

// PlaylistNames keeps the saved names of tagged playlists up to date. Spotify access is per user,
// so names are looked up with the access of whoever tags a playlist or adds a song to it.
// Names are cosmetic: lookups never prompt anyone to connect Spotify, and failures keep the old name.
// A nil *PlaylistNames does nothing.
type PlaylistNames struct {
	fetcher PlaylistNameFetcher
	store   PlaylistNameStore

	mu        sync.Mutex
	refreshed map[string]time.Time // When each playlist's name was last looked up
	lookups   map[string]uint64    // How many lookups of each playlist's name have started
}

// NewPlaylistNames creates a PlaylistNames, or returns nil if names can't be looked up or saved
func NewPlaylistNames(fetcher PlaylistNameFetcher, store PlaylistNameStore) *PlaylistNames {
	if fetcher == nil || store == nil {
		return nil
	}
	return &PlaylistNames{
		fetcher:   fetcher,
		store:     store,
		refreshed: make(map[string]time.Time),
		lookups:   make(map[string]uint64),
	}
}

// Refresh looks up a tagged playlist's name and saves it
func (p *PlaylistNames) Refresh(ctx context.Context, userID, playlistID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.refreshed[playlistID] = time.Now()
	p.mu.Unlock()
	p.refresh(ctx, userID, playlistID)
}

// RefreshStale refreshes the names of the tagged playlists among playlistIDs that weren't looked up
// in the last playlistNameRefreshInterval, so renames on Spotify show up without a lookup per song
func (p *PlaylistNames) RefreshStale(ctx context.Context, userID string, playlistIDs []string) {
	if p == nil {
		return
	}
	tagged := slices.Collect(maps.Values(p.store.Config().Tags))
	for _, id := range playlistIDs {
		if !slices.Contains(tagged, id) {
			continue
		}
		p.mu.Lock()
		stale := time.Since(p.refreshed[id]) >= playlistNameRefreshInterval
		if stale {
			p.refreshed[id] = time.Now()
		}
		p.mu.Unlock()
		if stale {
			p.refresh(ctx, userID, id)
		}
	}
}

// refresh looks up a playlist's name and saves it, keeping the saved name on errors.
// If lookups of the same playlist overlap, only the last one to start is saved, so a stale name can't win.
func (p *PlaylistNames) refresh(ctx context.Context, userID, playlistID string) {
	p.mu.Lock()
	p.lookups[playlistID]++
	lookup := p.lookups[playlistID]
	p.mu.Unlock()

	fields := []zap.Field{zap.String(zapkey.PlaylistID, log.RedactID(playlistID)), zap.String(zapkey.UserID, userID)}
	ctx, cancel := context.WithTimeout(ctx, playlistNameTimeout)
	defer cancel()
	name, err := p.fetcher.FetchPlaylistName(ctx, userID, playlistID)
	if err != nil {
		logger.Info("Couldn't look up the playlist name; keeping the saved one", append(fields, zap.Error(err))...)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lookups[playlistID] != lookup {
		return // A newer lookup started meanwhile
	}
	if err := p.store.SetPlaylistName(playlistID, name); err != nil {
		logger.Warn("Failed to save the playlist name", append(fields, zap.Error(err))...)
	}
}
