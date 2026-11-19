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
	// playlistNameTimeout limits how long a name lookup may take in the background
	playlistNameTimeout = 10 * time.Second

	// playlistNameLookupTimeout limits how long a reply waits for missing names. It's short,
	// since the user is waiting, and names are cosmetic: replies fall back to generic labels.
	playlistNameLookupTimeout = 3 * time.Second

	// maxPlaylistNameLookups is how many names Names looks up at once
	maxPlaylistNameLookups = 4

	// playlistNameRefreshInterval is how often adding a song refreshes its playlist's name
	playlistNameRefreshInterval = time.Hour
)

// PlaylistNameFetcher looks up a playlist's current name on Spotify, with a Discord user's Spotify access.
// It must never start the auth flow or post to Discord.
type PlaylistNameFetcher interface {
	FetchPlaylistName(ctx context.Context, userID, playlistID string) (string, error)
}

// PlaylistNameStore saves the names of tagged playlists and the default playlist
type PlaylistNameStore interface {
	Config() *trigger.Config
	PlaylistName(id string) string
	SetPlaylistNames(names map[string]string) error // Saves several names at once
}

// PlaylistNames looks up and saves the names of tagged playlists and the default playlist.
// Spotify access is per user, so names are looked up with the access of whoever runs a tag command
// or adds a song. Names are cosmetic: lookups never prompt anyone to connect Spotify, and failures
// keep the saved name. Everything that shows a playlist's name gets it through Name or Names.
// A nil *PlaylistNames only returns saved names, without looking anything up.
type PlaylistNames struct {
	fetcher PlaylistNameFetcher
	store   PlaylistNameStore

	mu        sync.Mutex
	refreshed map[string]time.Time // When each playlist's name was last looked up successfully, or is being refreshed
	lookups   map[string]uint64    // How many lookups of each playlist's name have started
	saved     map[string]uint64    // Which lookup of each playlist's name was last saved (see lookups)
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
		saved:     make(map[string]uint64),
	}
}

// Saved returns the playlist's saved name, or "" if it isn't known, without looking it up
func (p *PlaylistNames) Saved(playlistID string) string {
	if p == nil || playlistID == "" {
		return ""
	}
	return p.store.PlaylistName(playlistID)
}

// Name returns the playlist's saved name. If none is saved, it looks the name up with userID's
// Spotify access, within playlistNameLookupTimeout, and saves it if no name was saved meanwhile.
// It returns "" if the name is still unknown, e.g. if the user hasn't connected Spotify.
func (p *PlaylistNames) Name(ctx context.Context, userID, playlistID string) string {
	return p.Names(ctx, userID, []string{playlistID})[playlistID]
}

// Names is Name for several playlists, looking up the missing names in parallel (at most
// maxPlaylistNameLookups at once) and saving them together. Lookups that haven't started by the
// timeout are skipped. Unknown names are left out of the result.
func (p *PlaylistNames) Names(ctx context.Context, userID string, playlistIDs []string) map[string]string {
	names := make(map[string]string, len(playlistIDs))
	var missing []string
	for _, id := range playlistIDs {
		if name := p.Saved(id); name != "" {
			names[id] = name
		} else if id != "" && !slices.Contains(missing, id) {
			missing = append(missing, id)
		}
	}
	if p == nil || len(missing) == 0 {
		return names
	}

	ctx, cancel := context.WithTimeout(ctx, playlistNameLookupTimeout)
	defer cancel()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		found   = make(map[string]nameLookup)
		limiter = make(chan struct{}, maxPlaylistNameLookups)
	)
	for _, id := range missing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case limiter <- struct{}{}:
				defer func() { <-limiter }()
			case <-ctx.Done():
				return
			}
			if ctx.Err() != nil { // Both were ready, and select picked the limiter
				return
			}
			if result, ok := p.fetch(ctx, userID, id); ok {
				mu.Lock()
				found[id] = result
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	p.save(found, false)
	for id, result := range found {
		if result.name != "" {
			names[id] = result.name
		}
	}
	return names
}

// RefreshStale refreshes the names of the tagged playlists and the default playlist among playlistIDs
// that weren't looked up in the last playlistNameRefreshInterval, replacing the saved names, so renames
// on Spotify show up without a lookup per song. A failed refresh can be retried right away.
func (p *PlaylistNames) RefreshStale(ctx context.Context, userID string, playlistIDs []string) {
	if p == nil {
		return
	}
	c := p.store.Config()
	tagged := slices.Collect(maps.Values(c.Tags))
	for _, id := range playlistIDs {
		if id != c.DefaultPlaylistID && !slices.Contains(tagged, id) {
			continue
		}
		// Claimed before the lookup, so concurrent song adds don't refresh the same playlist twice
		p.mu.Lock()
		previous := p.refreshed[id]
		stale := time.Since(previous) >= playlistNameRefreshInterval
		claim := time.Now()
		if stale {
			p.refreshed[id] = claim
		}
		p.mu.Unlock()
		if !stale {
			continue
		}
		func() {
			ctx, cancel := context.WithTimeout(ctx, playlistNameTimeout)
			defer cancel()
			if result, ok := p.fetch(ctx, userID, id); ok {
				p.save(map[string]nameLookup{id: result}, true)
				return
			}
			p.mu.Lock()
			if p.refreshed[id].Equal(claim) {
				p.refreshed[id] = previous
			}
			p.mu.Unlock()
		}()
	}
}

// nameLookup is a playlist name that was looked up
type nameLookup struct {
	name   string
	lookup uint64 // Which lookup of the playlist's name found it (see PlaylistNames.lookups)
}

// fetch looks up a playlist's name, numbering the lookup. It reports false on errors.
func (p *PlaylistNames) fetch(ctx context.Context, userID, playlistID string) (nameLookup, bool) {
	p.mu.Lock()
	p.lookups[playlistID]++
	lookup := p.lookups[playlistID]
	p.mu.Unlock()

	name, err := p.fetcher.FetchPlaylistName(ctx, userID, playlistID)
	if err != nil {
		logger.Info("Couldn't look up the playlist name; keeping the saved one",
			zap.String(zapkey.PlaylistID, log.RedactID(playlistID)), zap.String(zapkey.UserID, userID), zap.Error(err))
		return nameLookup{}, false
	}
	return nameLookup{name: name, lookup: lookup}, true
}

// save saves names that were looked up, in one write: always if replace is true, and otherwise only
// if no name is saved. If lookups of the same playlist overlap, a result is only saved if no later
// lookup's result was saved already, so a stale name can't win, while a later lookup that fails
// doesn't discard an earlier one that succeeded.
func (p *PlaylistNames) save(results map[string]nameLookup, replace bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	batch := make(map[string]string, len(results))
	for id, result := range results {
		p.refreshed[id] = now
		if result.lookup <= p.saved[id] || result.name == "" {
			continue
		}
		if !replace && p.store.PlaylistName(id) != "" {
			continue
		}
		batch[id] = result.name
	}
	if len(batch) == 0 {
		return
	}
	if err := p.store.SetPlaylistNames(batch); err != nil {
		logger.Warn("Failed to save the playlist names",
			zap.Strings(zapkey.PlaylistIDs, log.RedactIDs(slices.Sorted(maps.Keys(batch)))), zap.Error(err))
		return
	}
	for id := range batch {
		p.saved[id] = results[id].lookup
	}
}
