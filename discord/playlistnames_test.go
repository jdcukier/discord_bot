package discord

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/spotify/trigger"
	"discordbot/spotify/worker"
)

// fakeNameFetcher returns names from a map, or err
type fakeNameFetcher struct {
	mu    sync.Mutex
	names map[string]string
	err   error
	calls []string // Playlist IDs looked up

	wait map[int]chan struct{} // Calls (counted from 0) that block until their channel is closed
}

func (f *fakeNameFetcher) FetchPlaylistName(_ context.Context, _, playlistID string) (string, error) {
	f.mu.Lock()
	call := len(f.calls)
	f.calls = append(f.calls, playlistID)
	name, err := f.names[playlistID], f.err
	wait := f.wait[call]
	f.mu.Unlock()
	if wait != nil {
		<-wait
	}
	return name, err
}

// lookups returns the playlist IDs looked up so far
func (f *fakeNameFetcher) lookups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func TestPlaylistNamesLastLookupWins(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	release := make(chan struct{})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Old"}, wait: map[int]chan struct{}{0: release}}
	names := NewPlaylistNames(fetcher, store)

	// The first lookup gets the old name but finishes last
	done := make(chan struct{})
	go func() {
		names.Refresh(context.Background(), "user-id", gymTestID)
		close(done)
	}()
	require.Eventually(t, func() bool { return len(fetcher.lookups()) == 1 }, time.Second, time.Millisecond)
	fetcher.mu.Lock()
	fetcher.names[gymTestID] = "New"
	fetcher.mu.Unlock()
	names.Refresh(context.Background(), "user-id", gymTestID)
	assert.Equal(t, "New", store.PlaylistName(gymTestID))

	close(release)
	<-done
	assert.Equal(t, "New", store.PlaylistName(gymTestID))
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

// executeAddTracksWithNames adds a song tagged +gym, with addErr as the adder's result
func executeAddTracksWithNames(t *testing.T, addErr error) (*fakeNameFetcher, *trigger.Store) {
	t.Helper()
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}}
	action := &AddTracksToPlaylist{
		session: &discordgo.Session{},
		event: &discordgo.MessageCreate{Message: &discordgo.Message{
			Content: testLink + " +gym",
			Author:  &discordgo.User{ID: "user-id"},
		}},
		playlistAdder: &fakePlaylistAdder{err: addErr},
		triggers:      store.Config(),
		names:         NewPlaylistNames(fetcher, store),
	}
	action.Execute(context.Background())
	return fetcher, store
}

func TestAddTracksRefreshesPlaylistNames(t *testing.T) {
	// In the background, so it doesn't hold up the message handler
	fetcher, store := executeAddTracksWithNames(t, nil)
	assert.Eventually(t, func() bool { return store.PlaylistName(gymTestID) == "Gym" }, time.Second, time.Millisecond)
	assert.Equal(t, []string{gymTestID}, fetcher.lookups(), "the default playlist has no tags")
}

func TestAddTracksSkipsPlaylistNamesUnlessAdded(t *testing.T) {
	// Queued until the user connects Spotify, or failed: a lookup would fail too, or use up the hourly one
	for _, err := range []error{worker.ErrAuthPending, errors.New("spotify is down")} {
		fetcher, _ := executeAddTracksWithNames(t, err)
		assert.Never(t, func() bool { return len(fetcher.lookups()) > 0 }, 50*time.Millisecond, time.Millisecond, err)
	}
}
