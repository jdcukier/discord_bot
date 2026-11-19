package discord

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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

// testPlaylistTarget is a playlist's link target as tag replies show it
func testPlaylistTarget(id string) string {
	return "(<https://open.spotify.com/playlist/" + id + ">)"
}

func TestSetTag(t *testing.T) {
	store := newTestTagStore(t, nil)

	// A new tag gets the /tag add reply. Its playlist's name isn't known yet.
	reply := setTag(store, " !Gym ", "https://open.spotify.com/playlist/"+gymTestID+"?si=x")
	assert.False(t, reply.private)
	assert.Equal(t, "✅ **Added `!gym`**\n"+
		"🎧 **[Untitled playlist]"+testPlaylistTarget(gymTestID)+"** — **`gym`**\n"+
		"-# `+gym` also adds to the default · Playlist must be collaborative", reply.content)
	assert.Equal(t, gymTestID, reply.playlistID)

	// Moving a tag shows where it was, with the old playlist's name escaped
	require.NoError(t, store.SetPlaylistName(gymTestID, "Gym *Bangers*"))
	reply = setTag(store, "+gym", chillTestID)
	assert.False(t, reply.private)
	assert.Equal(t, "🔁 **Moved `!gym`**\n"+
		"🎧 **[Untitled playlist]"+testPlaylistTarget(chillTestID)+"** — **`gym`**\n"+
		"-# Was: [Gym \\*Bangers\\*]"+testPlaylistTarget(gymTestID)+" · `+gym` also adds to the default · Playlist must be collaborative",
		reply.content)
	assert.Equal(t, chillTestID, reply.playlistID)
	// The old playlist has no tags left, so its name is forgotten
	assert.Empty(t, store.PlaylistName(gymTestID))

	// A playlist another tag already uses doesn't need the reminder
	require.NoError(t, store.SetPlaylistName(chillTestID, "Chill [2024]"))
	reply = setTag(store, "workout", chillTestID)
	assert.Equal(t, "✅ **Added `!workout`**\n"+
		"🎧 **[Chill \\[2024\\]]"+testPlaylistTarget(chillTestID)+"** — `gym`, **`workout`**\n"+
		"-# `+workout` also adds to the default", reply.content)

	// The default playlist uses its /tags label, lists its reserved tag, and needs no subtext
	reply = setTag(store, "main", defaultTestID)
	assert.Equal(t, "✅ **Added `!main`**\n"+
		"🎧 **[Default playlist]"+testPlaylistTarget(defaultTestID)+"** — `default`, **`main`**", reply.content)
	require.NoError(t, store.SetPlaylistName(defaultTestID, "Everything"))
	reply = setTag(store, "gym", defaultTestID)
	assert.Equal(t, "🔁 **Moved `!gym`**\n"+
		"🎧 **[Everything]"+testPlaylistTarget(defaultTestID)+"** — `default`, **`gym`**, `main`\n"+
		"-# Was: [Chill \\[2024\\]]"+testPlaylistTarget(chillTestID), reply.content)

	reply = setTag(store, "gym", defaultTestID)
	assert.True(t, reply.private)
	assert.Equal(t, "`!gym` already adds to that playlist.", reply.content)

	assert.Equal(t, map[string]string{"gym": defaultTestID, "workout": chillTestID, "main": defaultTestID}, store.Config().Tags)
}

func TestAddTag(t *testing.T) {
	store := newTestTagStore(t, nil)

	reply := addTag(store, "!Gym", "https://open.spotify.com/playlist/"+gymTestID)
	assert.False(t, reply.private)
	assert.Equal(t, "✅ **Added `!gym`**\n"+
		"🎧 **[Untitled playlist]"+testPlaylistTarget(gymTestID)+"** — **`gym`**\n"+
		"-# `+gym` also adds to the default · Playlist must be collaborative", reply.content)
	assert.Equal(t, gymTestID, reply.playlistID)

	// Existing tags aren't replaced
	reply = addTag(store, "gym", chillTestID)
	assert.True(t, reply.private)
	assert.Equal(t, "`!gym` already adds to <https://open.spotify.com/playlist/"+gymTestID+">. Use `/tag set` to change its playlist.", reply.content)

	reply = addTag(store, "gym", gymTestID)
	assert.True(t, reply.private)
	assert.Contains(t, reply.content, "already adds to that playlist")

	// Aliases share a playlist, so it's already collaborative. Tags are sorted, with the new one in bold.
	require.NoError(t, store.SetPlaylistName(gymTestID, "Gym Bangers"))
	reply = addTag(store, "fitness", gymTestID)
	assert.False(t, reply.private)
	assert.Equal(t, "✅ **Added `!fitness`**\n"+
		"🎧 **[Gym Bangers]"+testPlaylistTarget(gymTestID)+"** — **`fitness`**, `gym`\n"+
		"-# `+fitness` also adds to the default", reply.content)

	reply = addTag(store, "default", gymTestID)
	assert.True(t, strings.HasPrefix(reply.content, "Couldn't add the tag: "))

	assert.Equal(t, map[string]string{"gym": gymTestID, "fitness": gymTestID}, store.Config().Tags)
}

func TestTagRepliesCustomPrefixWithoutNames(t *testing.T) {
	settings, err := trigger.New("?", defaultTestID, nil, false)
	require.NoError(t, err)
	store, _, err := trigger.OpenStore(filepath.Join(t.TempDir(), "tags.yaml"), settings,
		map[string][]string{"gym": {gymTestID}})
	require.NoError(t, err)
	require.NoError(t, store.SetPlaylistName(gymTestID, "Gym Bangers"))

	// A store that doesn't know names falls back to the /tags labels
	reply := addTag(namelessTagStore{store}, "?lifting", gymTestID)
	assert.Equal(t, "✅ **Added `?lifting`**\n"+
		"🎧 **[Untitled playlist]"+testPlaylistTarget(gymTestID)+"** — `gym`, **`lifting`**\n"+
		"-# `+lifting` also adds to the default", reply.content)
	reply = removeTag(namelessTagStore{store}, "lifting")
	assert.Equal(t, "🗑️ **Deleted `?lifting`**\n"+
		"🎧 **[Untitled playlist]"+testPlaylistTarget(gymTestID)+"** — `gym`\n"+
		"-# Songs tagged `?lifting` now go to the default playlist", reply.content)
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
	store := newTestTagStore(t, map[string][]string{
		"gym": {gymTestID}, "workout": {gymTestID}, "chill": {chillTestID}, "main": {defaultTestID},
	})
	require.NoError(t, store.SetPlaylistName(gymTestID, "Gym_Bangers"))

	reply := removeTag(store, "+GYM")
	assert.False(t, reply.private)
	assert.Equal(t, "🗑️ **Deleted `!gym`**\n"+
		"🎧 **[Gym\\_Bangers]"+testPlaylistTarget(gymTestID)+"** — `workout`\n"+
		"-# Songs tagged `!gym` now go to the default playlist", reply.content)
	assert.Empty(t, reply.playlistID)

	// The playlist's name is shown even though removing its last tag forgets it
	reply = removeTag(store, "workout")
	assert.Equal(t, "🗑️ **Deleted `!workout`**\n"+
		"🎧 **[Gym\\_Bangers]"+testPlaylistTarget(gymTestID)+"** — no tags left\n"+
		"-# Songs tagged `!workout` now go to the default playlist", reply.content)
	assert.Empty(t, store.PlaylistName(gymTestID))

	reply = removeTag(store, "chill")
	assert.Equal(t, "🗑️ **Deleted `!chill`**\n"+
		"🎧 **[Untitled playlist]"+testPlaylistTarget(chillTestID)+"** — no tags left\n"+
		"-# Songs tagged `!chill` now go to the default playlist", reply.content)

	// The default playlist always keeps its reserved tag
	reply = removeTag(store, "main")
	assert.Equal(t, "🗑️ **Deleted `!main`**\n"+
		"🎧 **[Default playlist]"+testPlaylistTarget(defaultTestID)+"** — `default`\n"+
		"-# Songs tagged `!main` now go to the default playlist", reply.content)

	reply = removeTag(store, "gym")
	assert.True(t, reply.private)
	assert.True(t, strings.HasPrefix(reply.content, "Couldn't remove the tag: "))
	assert.Empty(t, store.Config().Tags)
}

func TestTagChangeReplyFitsInAMessage(t *testing.T) {
	many := map[string][]string{}
	for i := range 80 {
		many[fmt.Sprintf("a-rather-long-tag-name-number-%02d", i)] = []string{gymTestID}
	}
	store := newTestTagStore(t, many)
	reply := addTag(store, "zzz", gymTestID)
	assert.LessOrEqual(t, len(reply.content), maxMessageLength)
	lines := strings.Split(reply.content, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "✅ **Added `!zzz`**", lines[0])
	// The last tags are left out whole, keeping the header and subtext
	assert.True(t, strings.HasPrefix(lines[1], "🎧 **[Untitled playlist]"+testPlaylistTarget(gymTestID)+"** — `a-rather-long-tag-name-number-00`, "))
	assert.True(t, strings.HasSuffix(lines[1], "`, …"), lines[1])
	assert.Equal(t, "-# `+zzz` also adds to the default", lines[2])

	// Without room for a single tag, the line is cut
	assert.Equal(t, "🎧 **[A]…", fitPlaylistLine("🎧 **[A](<https://open.spotify.com/playlist/x>)** — `a`, `b`", 13))
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
		{"second tag on the default playlist", map[string][]string{"run": {gymTestID}, "main": {defaultTestID}}, "run", "main"},
		{"other playlist preferred over the default", map[string][]string{"run": {gymTestID}, "main": {defaultTestID}, "zen": {chillTestID}}, "run", "zen"},
		{"fallbacks are aliases", map[string][]string{"gym": {gymTestID}, "chill": {gymTestID}}, "chill", "party"},
		{"fallbacks on the default playlist", map[string][]string{"gym": {defaultTestID}, "chill": {defaultTestID}}, "party", "chill"},
		{"all fallbacks are aliases", map[string][]string{"gym": {gymTestID}, "chill": {gymTestID}, "party": {gymTestID}}, "chill", "gym"},
		{"all fallbacks on the default playlist", map[string][]string{"gym": {defaultTestID}, "chill": {defaultTestID}, "party": {defaultTestID}}, "chill", "gym"},
	} {
		first, second := exampleTagNames(newTestTagStore(t, tc.tags).Config())
		assert.Equal(t, tc.first, first, tc.name)
		assert.Equal(t, tc.second, second, tc.name)
	}
}

func TestEscapeMarkdown(t *testing.T) {
	assert.Equal(t, "Plain name", escapeMarkdown("Plain name"))
	assert.Equal(t, `\[a\]\*b\*\_c\_\`+"`d\\`"+`\~e\~\|f\|\>g\\h`, escapeMarkdown("[a]*b*_c_`d`~e~|f|>g\\h"))
}

func TestCleanPlaylistName(t *testing.T) {
	assert.Equal(t, "Plain name", cleanPlaylistName("  Plain name "))
	// Line breaks, separators and other control characters become spaces
	lineSep, paraSep := string(rune(0x2028)), string(rune(0x2029))
	assert.Equal(t, "a b c d e f", cleanPlaylistName("a\nb\r\nc"+lineSep+"d"+paraSep+"e\tf\x00"))
	// Invisible characters (zero-width space, bidi marks, override and isolate, BOM) are dropped
	zeroWidth := string(rune(0x200b))
	assert.Equal(t, "abcdef", cleanPlaylistName("a"+zeroWidth+"b"+string(rune(0x202e))+"c"+string(rune(0x2066))+
		"d"+string(rune(0xfeff))+"e"+string([]rune{0x200e, 0x200f})+"f"))
	assert.Equal(t, "ab", cleanPlaylistName("a"+string(rune(0x061c))+"b"), "Arabic letter mark")
	// Only invisible characters: treated as unknown
	assert.Equal(t, "", cleanPlaylistName(zeroWidth+"\n"))

	// Joiners and emoji tag characters are kept
	for _, name := range []string{
		string([]rune{0x1F469, 0x200D, 0x1F4BB}) + " coding",                                       // Woman technologist (ZWJ sequence)
		string([]rune{0x1F3F4, 0xE0067, 0xE0062, 0xE0065, 0xE006E, 0xE0067, 0xE007F}) + " England", // Flag (tag sequence)
		string([]rune{0x0645, 0x06CC, 0x200C, 0x062E, 0x0648, 0x0627, 0x0647, 0x0645}),             // Persian word with a ZWNJ
	} {
		assert.Equal(t, name, cleanPlaylistName(name))
	}

	// Long names are cut on a character boundary
	long := strings.Repeat("é", maxPlaylistNameLength+5)
	got := cleanPlaylistName(long)
	assert.Equal(t, maxPlaylistNameLength, utf8.RuneCountInString(got))
	assert.Equal(t, strings.Repeat("é", maxPlaylistNameLength-1)+"…", got)
	exact := strings.Repeat("x", maxPlaylistNameLength)
	assert.Equal(t, exact, cleanPlaylistName(exact))

	// A ZWJ sequence or an accent straddling the limit is dropped whole
	pad := strings.Repeat("x", maxPlaylistNameLength-3)
	emoji := string([]rune{0x1F469, 0x200D, 0x1F4BB})
	assert.Equal(t, pad+"…", cleanPlaylistName(pad+emoji+"y"))
	pad = strings.Repeat("x", maxPlaylistNameLength-2)
	assert.Equal(t, pad+"…", cleanPlaylistName(pad+"e"+string(rune(0x0301))+"z"))
	// An emoji with a skin tone modifier
	assert.Equal(t, pad+"…", cleanPlaylistName(pad+string([]rune{0x1F44D, 0x1F3FD})+"z"))
	// A spacing combining mark (Devanagari "ki")
	assert.Equal(t, pad+"…", cleanPlaylistName(pad+string([]rune{0x0915, 0x093F})+"z"))

	// Flags (pairs of regional indicators) aren't split
	fr, de := string([]rune{0x1F1EB, 0x1F1F7}), string([]rune{0x1F1E9, 0x1F1EA})
	assert.Equal(t, pad+"…", cleanPlaylistName(pad+fr+"abc"))
	pad = strings.Repeat("x", maxPlaylistNameLength-3)
	assert.Equal(t, pad+fr+"…", cleanPlaylistName(pad+fr+"abc"), "a whole flag fits")
	pad = strings.Repeat("x", maxPlaylistNameLength-4)
	assert.Equal(t, pad+fr+"…", cleanPlaylistName(pad+fr+de+"abc"), "the second flag straddles the limit")

	// Without a clean place to cut, the name is cut at the limit rather than dropped
	accent := string(rune(0x0301))
	got = cleanPlaylistName("a" + strings.Repeat(accent, 150))
	assert.Equal(t, "a"+strings.Repeat(accent, maxPlaylistNameLength-2)+"…", got)
}

func TestListTagsLongAndUnsafeNames(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}, "chill": {chillTestID}})
	names := testNames(map[string]string{
		gymTestID:   strings.Repeat(`\[`, 2000),
		chillTestID: "Late\nNight" + string(rune(0x202e)), // Ends with a bidi override
	})
	chunks := listTags(store.Config(), names)
	require.Len(t, chunks, 1)
	assert.LessOrEqual(t, len(chunks[0]), maxMessageLength)
	// The name is cut before escaping, so the link stays whole and no escape is split
	assert.Contains(t, chunks[0], "🎧 **["+strings.Repeat(`\\\[`, (maxPlaylistNameLength-1)/2)+`\\…](<https://open.spotify.com/playlist/`+gymTestID+">)** — `gym`")
	assert.Contains(t, chunks[0], "🎧 **[Late Night](<https://open.spotify.com/playlist/"+chillTestID+">)** — `chill`")
}

// namedTagStore is a tag store that knows playlist names
// namelessTagStore hides the store's playlist names
type namelessTagStore struct{ TagStore }

func TestPlaylistNames(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	// Stores that don't know names give no lookup
	assert.Nil(t, playlistNames(namelessTagStore{store}))

	// The tags file keeps the names
	name := playlistNames(store)
	require.NotNil(t, name)
	assert.Empty(t, name(gymTestID))
	require.NoError(t, store.SetPlaylistName(gymTestID, "Gym Bangers"))
	assert.Equal(t, "Gym Bangers", name(gymTestID))
	assert.Contains(t, listTags(store.Config(), name)[0], "🎧 **[Gym Bangers](<https://open.spotify.com/playlist/"+gymTestID+">)** — `gym`")
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
