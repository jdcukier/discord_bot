package discord

import (
	"fmt"
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
	settings, err := trigger.New("!", defaultTestID, nil, false)
	require.NoError(t, err)
	store, _, err := trigger.OpenStore(filepath.Join(t.TempDir(), "tags.yaml"), settings, playlists)
	require.NoError(t, err)
	return store
}

func TestSetTag(t *testing.T) {
	store := newTestTagStore(t, nil)

	reply := setTag(store, " !Gym ", "https://open.spotify.com/playlist/"+gymTestID+"?si=x")
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "`!gym` now adds songs to <https://open.spotify.com/playlist/"+gymTestID+">, and `+gym` adds them to the default playlist too.")
	assert.Contains(t, reply.content, "collaborative")
	assert.NotContains(t, reply.content, "used to")

	// Replacing the playlist
	reply = setTag(store, "+gym", chillTestID)
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "It used to add to <https://open.spotify.com/playlist/"+gymTestID+">.")

	// A playlist another tag already uses doesn't need the reminder
	reply = setTag(store, "workout", chillTestID)
	assert.NotContains(t, reply.content, "collaborative")
	reply = setTag(store, "main", defaultTestID)
	assert.Contains(t, reply.content, "now adds songs to the default playlist")
	assert.NotContains(t, reply.content, "collaborative")

	reply = setTag(store, "gym", chillTestID)
	assert.True(t, reply.private)
	assert.Contains(t, reply.content, "already")

	assert.Equal(t, map[string]string{"gym": chillTestID, "workout": chillTestID, "main": defaultTestID}, store.Config().Tags)
}

func TestAddTag(t *testing.T) {
	store := newTestTagStore(t, nil)

	reply := addTag(store, "!Gym", "https://open.spotify.com/playlist/"+gymTestID)
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "`!gym` now adds songs to <https://open.spotify.com/playlist/"+gymTestID+">, and `+gym` adds them to the default playlist too.")
	assert.Contains(t, reply.content, "collaborative")

	// Existing tags aren't replaced
	reply = addTag(store, "gym", chillTestID)
	assert.True(t, reply.private)
	assert.Equal(t, "`!gym` already adds to <https://open.spotify.com/playlist/"+gymTestID+">. Use `/tag set` to change its playlist.", reply.content)

	reply = addTag(store, "gym", gymTestID)
	assert.True(t, reply.private)
	assert.Contains(t, reply.content, "already adds to that playlist")

	// Aliases share a playlist
	reply = addTag(store, "workout", gymTestID)
	assert.False(t, reply.private)
	assert.NotContains(t, reply.content, "collaborative")

	reply = addTag(store, "default", gymTestID)
	assert.True(t, strings.HasPrefix(reply.content, "Couldn't add the tag: "))

	assert.Equal(t, map[string]string{"gym": gymTestID, "workout": gymTestID}, store.Config().Tags)
}

func TestSetTagErrors(t *testing.T) {
	store := newTestTagStore(t, nil)
	for _, tc := range []struct{ name, playlist string }{
		{"default", gymTestID},
		{"two words", gymTestID},
		{"gym", "https://open.spotify.com/track/" + gymTestID},
		{"gym", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"},
	} {
		reply := setTag(store, tc.name, tc.playlist)
		assert.True(t, reply.private, tc)
		assert.True(t, strings.HasPrefix(reply.content, "Couldn't set the tag: "), tc)
	}
	assert.Empty(t, store.Config().Tags)
}

func TestRemoveTag(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}, "chill": {chillTestID}})

	reply := removeTag(store, "+GYM")
	assert.False(t, reply.private)
	assert.Contains(t, reply.content, "Deleted `!gym`")

	reply = removeTag(store, "gym")
	assert.True(t, reply.private)
	assert.True(t, strings.HasPrefix(reply.content, "Couldn't remove the tag: "))
	assert.Equal(t, map[string]string{"chill": chillTestID}, store.Config().Tags)
}

func TestListTags(t *testing.T) {
	empty := newTestTagStore(t, nil)
	chunks := listTags(empty.Config())
	require.Len(t, chunks, 1)
	assert.Contains(t, chunks[0], "`+name` also adds it to the default playlist")
	assert.Contains(t, chunks[0], "**Default playlist** <https://open.spotify.com/playlist/"+defaultTestID+">: `!default`")
	assert.Contains(t, chunks[0], "No other tags yet")

	// Grouped by playlist, so aliases are listed together
	store := newTestTagStore(t, map[string][]string{
		"relaxing": {chillTestID},
		"chill":    {chillTestID},
		"gym":      {gymTestID},
		"main":     {defaultTestID},
	})
	chunks = listTags(store.Config())
	require.Len(t, chunks, 1)
	lines := strings.Split(chunks[0], "\n")
	assert.Equal(t, []string{
		"**Default playlist** <https://open.spotify.com/playlist/" + defaultTestID + ">: `!default`, `!main`",
		"<https://open.spotify.com/playlist/" + chillTestID + ">: `!chill`, `!relaxing`",
		"<https://open.spotify.com/playlist/" + gymTestID + ">: `!gym`",
	}, lines[1:])

	// Long lists are split without breaking lines
	many := map[string][]string{}
	ids := []string{gymTestID, chillTestID}
	for i, c := range "abcdefghijklmnopqrstuvwxyz0123456789" {
		many["a-very-long-tag-name-for-testing-"+string(c)] = []string{ids[i%2]}
	}
	for i := range 40 {
		id := fmt.Sprintf("%022d", i)
		many[fmt.Sprintf("tag%d", i)] = []string{id}
	}
	chunks = listTags(newTestTagStore(t, many).Config())
	assert.Greater(t, len(chunks), 1)
	for _, chunk := range chunks {
		assert.LessOrEqual(t, len(chunk), maxMessageLength)
	}
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
	assert.Equal(t, []string{"chill-gym", "gym"}, names("+gy"))
	// Never nil: Discord requires the choices field
	assert.NotNil(t, autocompleteTags(store.Config(), "zzz"))
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

func TestChunkLinesSplitsLongLines(t *testing.T) {
	long := "`!big` → " + strings.Repeat("<https://open.spotify.com/playlist/"+gymTestID+">, ", 60)
	long = strings.TrimSuffix(long, ", ")
	chunks := chunkLines([]string{"header", long}, maxMessageLength)
	require.Greater(t, len(chunks), 1)
	for _, chunk := range chunks {
		assert.LessOrEqual(t, len(chunk), maxMessageLength)
	}
	// Splits happen after a comma, so no link is broken
	assert.Equal(t, 60, strings.Count(strings.Join(chunks, "\n"), "https://open.spotify.com/playlist/"+gymTestID+">"))

	// A line without commas is split at a character boundary
	chunks = chunkLines([]string{strings.Repeat("é", 1500)}, maxMessageLength)
	require.Len(t, chunks, 2)
	assert.Equal(t, strings.Repeat("é", 1500), chunks[0]+chunks[1])
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 10))
	got := truncate(strings.Repeat("é", 10), 9)
	assert.LessOrEqual(t, len(got), 9)
	assert.Equal(t, "ééé…", got)
}
