package discord

import (
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/spotify/trigger"
)

const testLink = "https://open.spotify.com/track/6adFsinyuxpOTkN4QmwwOt?si=3f33d53164e54ceb"

// fakePlaylistAdder records each call to AddTracksToPlaylist
type fakePlaylistAdder struct {
	calls     [][]string // Playlist IDs of each call
	trackURLs [][]string // Track URLs of each call
}

func (f *fakePlaylistAdder) AddTracksToPlaylist(_ context.Context, _ string, trackURLs []string, playlistIDs ...string) error {
	f.calls = append(f.calls, playlistIDs)
	f.trackURLs = append(f.trackURLs, trackURLs)
	return nil
}

func executeAddTracks(t *testing.T, triggers *trigger.Config, content string) *fakePlaylistAdder {
	t.Helper()
	adder := &fakePlaylistAdder{}
	action := &AddTracksToPlaylist{
		session: &discordgo.Session{},
		event: &discordgo.MessageCreate{Message: &discordgo.Message{
			Content: content,
			Author:  &discordgo.User{ID: "user-id"},
		}},
		playlistAdder: adder,
		triggers:      triggers,
	}
	action.Execute(context.Background())
	return adder
}

func TestAddTracksToPlaylistWithTriggers(t *testing.T) {
	triggers, err := trigger.New("!", "default-id", map[string][]string{
		"relaxing": {"relax-id", "chill-id"},
		"chill":    {"chill-id"},
	}, false)
	require.NoError(t, err)

	tests := map[string][][]string{
		testLink:                         {{"default-id"}},
		testLink + " check this out!":    {{"default-id"}},
		testLink + " !relaxing":          {{"relax-id", "chill-id"}},
		testLink + " !default !relaxing": {{"default-id", "relax-id", "chill-id"}},
		testLink + " !relaxing !chill":   {{"relax-id", "chill-id"}},
		testLink + " !unknown":           {{"default-id"}},
		"no link here !relaxing":         nil,
	}
	for content, want := range tests {
		assert.Equal(t, want, executeAddTracks(t, triggers, content).calls, content)
	}
}

func TestAddTracksToPlaylistPassesTracks(t *testing.T) {
	triggers, err := trigger.New("!", "default-id", map[string][]string{"gym": {"gym-id"}}, false)
	require.NoError(t, err)

	other := "https://open.spotify.com/track/0VjIjW4GlUZAMYd2vXMi3b"
	adder := executeAddTracks(t, triggers, testLink+" !gym and "+other)
	assert.Equal(t, [][]string{{"gym-id"}}, adder.calls)
	assert.Equal(t, [][]string{{testLink, other}}, adder.trackURLs)
}

func TestAddTracksToPlaylistWithoutTriggers(t *testing.T) {
	t.Setenv("SPOTIFY_PLAYLIST_ID", "env-default-id")
	adder := executeAddTracks(t, nil, testLink+" !relaxing")
	assert.Equal(t, [][]string{{"env-default-id"}}, adder.calls)
}
