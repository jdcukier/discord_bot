package discord

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/spotify/trigger"
)

const (
	defaultTestID = "DEFAULT000000000000000"
	gymTestID     = "0123456789abcdefghijkl"
	chillTestID   = "ABCDEFGHIJKLMNOPQRSTUV"
)

func newTestTagStore(t *testing.T, playlists map[string][]string) *trigger.Store {
	t.Helper()
	base, err := trigger.New("!", defaultTestID, playlists, false)
	require.NoError(t, err)
	store, _, err := trigger.OpenStore(filepath.Join(t.TempDir(), "tags.json"), base)
	require.NoError(t, err)
	return store
}

func TestAddTag(t *testing.T) {
	store := newTestTagStore(t, nil)

	reply := addTag(store, " !Gym ", "https://open.spotify.com/playlist/"+gymTestID+"?si=x")
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "`!gym` now adds songs to <https://open.spotify.com/playlist/"+gymTestID+">")
	assert.Contains(t, reply.content, "collaborative")

	reply = addTag(store, "gym", defaultTestID)
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, gymTestID+">, the default playlist")
	assert.NotContains(t, reply.content, "collaborative")

	reply = addTag(store, "gym", gymTestID)
	assert.True(t, reply.private)
	assert.Contains(t, reply.content, "already")

	assert.Equal(t, map[string][]string{"gym": {gymTestID, defaultTestID}}, store.Config().Playlists)
}

func TestAddTagErrors(t *testing.T) {
	store := newTestTagStore(t, nil)
	for _, tc := range []struct{ name, playlist string }{
		{"default", gymTestID},
		{"two words", gymTestID},
		{"gym", "https://open.spotify.com/track/" + gymTestID},
		{"gym", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"},
	} {
		reply := addTag(store, tc.name, tc.playlist)
		assert.True(t, reply.private, tc)
		assert.True(t, strings.HasPrefix(reply.content, "Couldn't add the tag: "), tc)
	}
	assert.Empty(t, store.Config().Playlists)
}

func TestRemoveTag(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{
		"gym":   {gymTestID, chillTestID},
		"chill": {chillTestID},
		"old":   {"37i9dQZF1DXcBWIGoYBM5M"},
	})

	reply := removeTag(store, "!GYM", "https://open.spotify.com/playlist/"+chillTestID)
	assert.False(t, reply.private)
	assert.Equal(t, "`!gym` now adds songs to <https://open.spotify.com/playlist/"+gymTestID+">", reply.content)

	reply = removeTag(store, "chill", "")
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "Deleted `!chill`")

	// Editorial playlists can't be added but can be removed
	reply = removeTag(store, "old", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M")
	assert.Contains(t, reply.content, "Deleted `!old`")

	for _, tc := range []struct{ name, playlist string }{
		{"chill", ""},           // already deleted
		{"gym", chillTestID},    // not in the tag
		{"gym", "not a link/x"}, // invalid link
	} {
		reply := removeTag(store, tc.name, tc.playlist)
		assert.True(t, reply.private, tc)
		assert.True(t, strings.HasPrefix(reply.content, "Couldn't change the tag: "), tc)
	}
	assert.Equal(t, map[string][]string{"gym": {gymTestID}}, store.Config().Playlists)
}

func TestListTags(t *testing.T) {
	empty := newTestTagStore(t, nil)
	chunks := listTags(empty.Config())
	require.Len(t, chunks, 1)
	assert.Contains(t, chunks[0], "`!default` → <https://open.spotify.com/playlist/"+defaultTestID+">")
	assert.Contains(t, chunks[0], "No other tags yet")

	store := newTestTagStore(t, map[string][]string{
		"gym":   {defaultTestID, gymTestID},
		"chill": {chillTestID},
	})
	chunks = listTags(store.Config())
	require.Len(t, chunks, 1)
	lines := strings.Split(chunks[0], "\n")
	assert.Equal(t, []string{
		"`!chill` → <https://open.spotify.com/playlist/" + chillTestID + ">",
		"`!gym` → the default playlist, <https://open.spotify.com/playlist/" + gymTestID + ">",
	}, lines[2:])

	// Long lists are split without breaking lines
	many := map[string][]string{}
	for _, c := range "abcdefghijklmnopqrstuvwxyz0123456789" {
		many["tag"+string(c)] = []string{gymTestID, chillTestID}
	}
	chunks = listTags(newTestTagStore(t, many).Config())
	assert.Greater(t, len(chunks), 1)
	total := 0
	for _, chunk := range chunks {
		assert.LessOrEqual(t, len(chunk), maxMessageLength)
		total += strings.Count(chunk, "\n") + 1
	}
	assert.Equal(t, 2+len(many), total)
}

func TestAutocompleteTags(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{
		"gym": {gymTestID}, "chill": {chillTestID}, "chill-gym": {chillTestID},
	})
	names := func(typed string) []string {
		var out []string
		for _, c := range autocompleteTags(store.Config(), typed) {
			out = append(out, c.Value.(string))
		}
		return out
	}
	assert.Equal(t, []string{"chill", "chill-gym", "gym"}, names(""))
	assert.Equal(t, []string{"chill-gym", "gym"}, names("!GY"))
	assert.Empty(t, names("zzz"))
}

func TestTagCommandsDefinitions(t *testing.T) {
	for _, cmd := range tagCommands() {
		assert.LessOrEqual(t, len(cmd.Description), 100, cmd.Name)
		for _, sub := range cmd.Options {
			assert.LessOrEqual(t, len(sub.Description), 100, sub.Name)
			for _, opt := range sub.Options {
				assert.LessOrEqual(t, len(opt.Description), 100, opt.Name)
			}
		}
	}
}
