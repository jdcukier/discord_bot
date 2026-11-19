package discord

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/spotify/worker"
)

// fakeNameFetcher returns names from a map, or err
type fakeNameFetcher struct {
	mu    sync.Mutex
	names map[string]string
	err   error
	calls []string // Playlist IDs looked up
}

func (f *fakeNameFetcher) FetchPlaylistName(_ context.Context, _, playlistID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, playlistID)
	if f.err != nil {
		return "", f.err
	}
	return f.names[playlistID], nil
}

func TestPlaylistNamesRefresh(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym", chillTestID: "Chill"}}
	names := NewPlaylistNames(fetcher, store)

	names.Refresh(context.Background(), "user-id", gymTestID)
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))

	// Renamed on Spotify
	fetcher.names[gymTestID] = "Gym Bangers"
	names.Refresh(context.Background(), "user-id", gymTestID)
	assert.Equal(t, "Gym Bangers", store.PlaylistName(gymTestID))

	// Failures keep the saved name
	for _, err := range []error{worker.ErrAuthRequired, errors.New("spotify is down")} {
		fetcher.err = err
		names.Refresh(context.Background(), "user-id", gymTestID)
		assert.Equal(t, "Gym Bangers", store.PlaylistName(gymTestID))
	}
	fetcher.err = nil

	// Untagged playlists aren't saved
	names.Refresh(context.Background(), "user-id", chillTestID)
	assert.Empty(t, store.PlaylistName(chillTestID))
}

func TestPlaylistNamesRefreshStale(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}}
	names := NewPlaylistNames(fetcher, store)

	// Only tagged playlists are looked up, at most once an hour
	names.RefreshStale(context.Background(), "user-id", []string{defaultTestID, gymTestID})
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Equal(t, []string{gymTestID}, fetcher.calls)
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))
}

func TestPlaylistNamesNil(t *testing.T) {
	assert.Nil(t, NewPlaylistNames(nil, newTestTagStore(t, nil)))
	assert.Nil(t, NewPlaylistNames(&fakeNameFetcher{}, nil))

	var names *PlaylistNames
	names.Refresh(context.Background(), "user-id", gymTestID)
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
}

func TestAddTagNameLookupFails(t *testing.T) {
	store := newTestTagStore(t, nil)
	fetcher := &fakeNameFetcher{err: worker.ErrAuthRequired}
	names := NewPlaylistNames(fetcher, store)

	// The tag is added, and the reply asks for the playlist's name to be looked up afterwards
	reply := addTag(store, "gym", gymTestID)
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "now adds songs to")
	assert.Equal(t, gymTestID, reply.playlistID)

	// The lookup fails without changing the tag
	names.Refresh(context.Background(), "user-id", reply.playlistID)
	assert.Equal(t, []string{gymTestID}, fetcher.calls)
	assert.Equal(t, map[string]string{"gym": gymTestID}, store.Config().Tags)
	assert.Empty(t, store.PlaylistName(gymTestID))

	// Failed commands don't look anything up
	reply = addTag(store, "gym", chillTestID)
	assert.Empty(t, reply.playlistID)
	reply = setTag(store, "gym", "not a link")
	assert.Empty(t, reply.playlistID)
}

func TestSetTagLooksUpName(t *testing.T) {
	store := newTestTagStore(t, nil)
	names := NewPlaylistNames(&fakeNameFetcher{names: map[string]string{chillTestID: "Chill"}}, store)

	reply := setTag(store, "relax", chillTestID)
	require.Equal(t, chillTestID, reply.playlistID)
	names.Refresh(context.Background(), "user-id", reply.playlistID)
	assert.Equal(t, "Chill", store.PlaylistName(chillTestID))
}

func TestAddTracksRefreshesPlaylistNames(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}}
	action := &AddTracksToPlaylist{
		session: &discordgo.Session{},
		event: &discordgo.MessageCreate{Message: &discordgo.Message{
			Content: testLink + " +gym",
			Author:  &discordgo.User{ID: "user-id"},
		}},
		playlistAdder: &fakePlaylistAdder{},
		triggers:      store.Config(),
		names:         NewPlaylistNames(fetcher, store),
	}
	action.Execute(context.Background())
	assert.Equal(t, []string{gymTestID}, fetcher.calls, "the default playlist has no tags")
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))
}
