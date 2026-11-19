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
	store, _, err := trigger.OpenStore(filepath.Join(t.TempDir(), "tags.json"), settings, playlists)
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

// testNames returns playlist names from a map, and "" for unknown playlists
func testNames(names map[string]string) func(id string) string {
	return func(id string) string { return names[id] }
}

func TestListTagsEmpty(t *testing.T) {
	store := newTestTagStore(t, nil)
	chunks := listTags(store.Config(), nil)
	require.Len(t, chunks, 1)
	assert.Equal(t, strings.Join([]string{
		"## 🎵 Playlist tags",
		"> **How to tag**",
		"> `<song link> !gym` — add to that playlist only",
		"> `<song link> +gym` — add to that playlist **and** the default",
		"> `<song link> !gym !chill` — add to several",
		"> No tag → default playlist",
		"",
		"**Playlists**",
		"🎧 **[Default playlist](<https://open.spotify.com/playlist/" + defaultTestID + ">)** — `default`",
		"No other tags yet.",
		"",
		"> **Manage**",
		"> `/tag add` — create a tag",
		"> `/tag set` — create a tag or change its playlist",
		"> `/tag remove` — delete a tag",
	}, "\n"), chunks[0])
}

func TestListTags(t *testing.T) {
	const (
		lateTestID  = "LATE000000000000000000"
		otherTestID = "OTHER00000000000000000"
	)
	store := newTestTagStore(t, map[string][]string{
		"workout":  {gymTestID},
		"gym":      {gymTestID},
		"lifting":  {gymTestID},
		"sleep":    {lateTestID},
		"chill":    {chillTestID},
		"relaxing": {chillTestID},
		"main":     {defaultTestID},
		"misc":     {otherTestID},
	})
	names := testNames(map[string]string{
		gymTestID:   "Gym Bangers",
		lateTestID:  "late Night",
		chillTestID: "Chill *vibes* [2024]",
	})
	chunks := listTags(store.Config(), names)
	require.Len(t, chunks, 1)
	lines := strings.Split(chunks[0], "\n")

	// Real tags on two different playlists are used as examples, and the default playlist's tags aren't
	assert.Equal(t, "> `<song link> !chill` — add to that playlist only", lines[2])
	assert.Equal(t, "> `<song link> +chill` — add to that playlist **and** the default", lines[3])
	assert.Equal(t, "> `<song link> !chill !gym` — add to several", lines[4])

	// Playlists by name (case-insensitive) after the default one, with unknown names last
	assert.Equal(t, []string{
		"**Playlists**",
		"🎧 **[Default playlist](<https://open.spotify.com/playlist/" + defaultTestID + ">)** — `default`, `main`",
		"🎧 **[Chill \\*vibes\\* \\[2024\\]](<https://open.spotify.com/playlist/" + chillTestID + ">)** — `chill`, `relaxing`",
		"🎧 **[Gym Bangers](<https://open.spotify.com/playlist/" + gymTestID + ">)** — `gym`, `lifting`, `workout`",
		"🎧 **[late Night](<https://open.spotify.com/playlist/" + lateTestID + ">)** — `sleep`",
		"🎧 **[Untitled playlist](<https://open.spotify.com/playlist/" + otherTestID + ">)** — `misc`",
		"",
		"> **Manage**",
	}, lines[7:15])

	// The default playlist's name is shown when it's known
	chunks = listTags(store.Config(), testNames(map[string]string{defaultTestID: "Everything"}))
	assert.Contains(t, chunks[0], "🎧 **[Everything](<https://open.spotify.com/playlist/"+defaultTestID+">)** — `default`, `main`")
}

func TestListTagsSortsUnknownNamesByID(t *testing.T) {
	// The tag on chillTestID sorts first, but gymTestID ("0…") sorts before chillTestID ("A…")
	store := newTestTagStore(t, map[string][]string{"b": {gymTestID}, "a": {chillTestID}, "c": {defaultTestID}})
	chunks := listTags(store.Config(), testNames(nil))
	require.Len(t, chunks, 1)
	assert.Less(t, strings.Index(chunks[0], gymTestID), strings.Index(chunks[0], chillTestID))
	// Ties are broken by ID too
	chunks = listTags(store.Config(), func(string) string { return "Same" })
	assert.Less(t, strings.Index(chunks[0], gymTestID), strings.Index(chunks[0], chillTestID))
}

func TestExampleTagNames(t *testing.T) {
	for _, tc := range []struct {
		name          string
		tags          map[string][]string
		first, second string
	}{
		{"no tags", nil, "gym", "chill"},
		{"one tag", map[string][]string{"run": {gymTestID}}, "run", "gym"},
		{"one tag named like a fallback", map[string][]string{"gym": {gymTestID}}, "gym", "chill"},
		{"aliases only", map[string][]string{"chill": {chillTestID}, "relax": {chillTestID}}, "chill", "gym"},
		{"default playlist only", map[string][]string{"main": {defaultTestID}}, "gym", "chill"},
		{"two playlists", map[string][]string{"b": {gymTestID}, "a": {gymTestID}, "c": {chillTestID}}, "a", "c"},
	} {
		first, second := exampleTagNames(newTestTagStore(t, tc.tags).Config())
		assert.Equal(t, tc.first, first, tc.name)
		assert.Equal(t, tc.second, second, tc.name)
	}
}

func TestEscapeMarkdown(t *testing.T) {
	assert.Equal(t, "Plain name", escapeMarkdown("Plain name"))
	assert.Equal(t, `\[a\]\*b\*\_c\_\`+"`d\\`"+`\~e\~\|f\|\>g\\h`, escapeMarkdown("[a]*b*_c_`d`~e~|f|>g\\h"))
	assert.Equal(t, "one two three", escapeMarkdown("one\ntwo\r\nthree"))
}

func TestListTagsSplitsLongLists(t *testing.T) {
	many := map[string][]string{}
	ids := []string{gymTestID, chillTestID}
	for i, c := range "abcdefghijklmnopqrstuvwxyz0123456789" {
		many["a-very-long-tag-name-for-testing-"+string(c)] = []string{ids[i%2]}
	}
	names := map[string]string{}
	for i := range 40 {
		id := fmt.Sprintf("%022d", i)
		many[fmt.Sprintf("tag%d", i)] = []string{id}
		names[id] = fmt.Sprintf("Playlist number %02d with a fairly long name", i)
	}
	chunks := listTags(newTestTagStore(t, many).Config(), testNames(names))
	require.Greater(t, len(chunks), 1)
	for i, chunk := range chunks {
		assert.LessOrEqual(t, len(chunk), maxMessageLength)
		assert.False(t, strings.HasPrefix(chunk, "\n"), "chunk %d starts with a blank line", i)
		// "How to tag" is only in the first message, and "Manage" only in the last
		assert.Equal(t, i == 0, strings.Contains(chunk, "**How to tag**"), i)
		assert.Equal(t, i == len(chunks)-1, strings.Contains(chunk, "**Manage**"), i)
		assert.Equal(t, i == 0, strings.Contains(chunk, "**Playlists**"), i)
	}
	// The quoted blocks aren't split
	assert.Contains(t, chunks[0], "> **How to tag**\n> `<song link> !a-very-long-tag-name-for-testing-0`")
	assert.Contains(t, chunks[0], "> No tag → default playlist")
	assert.True(t, strings.HasSuffix(chunks[len(chunks)-1], "> **Manage**\n> `/tag add` — create a tag\n"+
		"> `/tag set` — create a tag or change its playlist\n> `/tag remove` — delete a tag"))
	// Every playlist is listed once, sorted by name
	all := strings.Join(chunks, "\n")
	assert.Equal(t, 43, strings.Count(all, "🎧 "))
	assert.Less(t, strings.Index(all, "Playlist number 00"), strings.Index(all, "Playlist number 39"))
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
