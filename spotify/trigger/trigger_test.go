package trigger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const link = "https://open.spotify.com/track/6adFsinyuxpOTkN4QmwwOt?si=3f33d53164e54ceb"

func newTestConfig(t *testing.T) *Config {
	t.Helper()
	playlists, err := ParsePlaylists("relaxing=relax-id,chill-id; chill=chill-id; Gym=gym-id")
	require.NoError(t, err)
	c, err := New(DefaultPrefix, "default-id", playlists, false)
	require.NoError(t, err)
	return c
}

func TestParsePlaylists(t *testing.T) {
	playlists, err := ParsePlaylists(" relaxing = a, b ;chill=b;RELAXING=c,a; ")
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"relaxing": {"a", "b", "c"},
		"chill":    {"b"},
	}, playlists)

	empty, err := ParsePlaylists("")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestParsePlaylistsErrors(t *testing.T) {
	for _, s := range []string{
		"relaxing",          // missing '='
		"relaxing=",         // no IDs
		"=id",               // no name
		"two words=id",      // invalid name
		"gym-=id",           // trailing hyphen
		"lo--fi=id",         // repeated separator
		"_gym=id",           // leading underscore
		"!relaxing=id",      // prefix included in name
		"relaxing=a;chill=", // one bad entry among good ones
	} {
		_, err := ParsePlaylists(s)
		assert.Error(t, err, s)
	}
}

func TestNewErrors(t *testing.T) {
	_, err := New("!", "", nil, false)
	assert.Error(t, err, "empty default playlist")

	_, err = New("!", "default-id", map[string][]string{DefaultName: {"x"}}, false)
	assert.Error(t, err, "reserved name")

	for _, prefix := range []string{"", "!!", "a", "1", " ", "_", "-"} {
		_, err = New(prefix, "default-id", nil, false)
		assert.Error(t, err, "prefix %q", prefix)
	}

	for _, prefix := range []string{"!", "$", "+", "&", "#", "."} {
		_, err = New(prefix, "default-id", nil, false)
		assert.NoError(t, err, "prefix %q", prefix)
	}
}

func TestLoad(t *testing.T) {
	t.Setenv("SPOTIFY_PLAYLIST_ID", "default-id")
	t.Setenv("SPOTIFY_PLAYLIST_TRIGGERS", "relaxing=relax-id")
	t.Setenv("SPOTIFY_TRIGGER_PREFIX", "")
	t.Setenv("SPOTIFY_REPLY_UNKNOWN_TRIGGERS", "")

	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, DefaultPrefix, c.Prefix)
	assert.Equal(t, "default-id", c.DefaultPlaylistID)
	assert.Equal(t, map[string][]string{"relaxing": {"relax-id"}}, c.Playlists)
	assert.False(t, c.ReplyUnknown)

	t.Setenv("SPOTIFY_TRIGGER_PREFIX", "$")
	t.Setenv("SPOTIFY_REPLY_UNKNOWN_TRIGGERS", "true")
	c, err = Load()
	require.NoError(t, err)
	assert.Equal(t, "$", c.Prefix)
	assert.True(t, c.ReplyUnknown)

	t.Setenv("SPOTIFY_REPLY_UNKNOWN_TRIGGERS", "sometimes")
	_, err = Load()
	assert.Error(t, err)
}

func TestLoadWithoutTriggers(t *testing.T) {
	t.Setenv("SPOTIFY_PLAYLIST_ID", "default-id")
	t.Setenv("SPOTIFY_PLAYLIST_TRIGGERS", "")
	t.Setenv("SPOTIFY_TRIGGER_PREFIX", "")
	t.Setenv("SPOTIFY_REPLY_UNKNOWN_TRIGGERS", "")

	c, err := Load()
	require.NoError(t, err)
	ids, unknown := c.Resolve(link + " !relaxing")
	assert.Equal(t, []string{"default-id"}, ids)
	assert.Equal(t, []string{"relaxing"}, unknown)
}

func TestExtract(t *testing.T) {
	c := newTestConfig(t)
	tests := map[string][]string{
		link:                              nil,
		link + " you gotta hear this!":    nil,
		link + " !relaxing":               {"relaxing"},
		"!Relaxing " + link:               {"relaxing"},
		link + " !relaxing, !gym.":        {"relaxing", "gym"},
		link + " !gym !GYM !gym":          {"gym"},
		link + "\n!chill":                 {"chill"},
		link + " wow!chill":               nil, // must start a word
		link + " !!":                      nil,
		link + " ! chill":                 nil,
		link + " !chill!gym":              {"chill"},
		link + " !lo-fi_2":                {"lo-fi_2"},
		link + " #relaxing":               nil,
		"https://example.com/!relaxing x": nil,
		link + " <#123456789>":            nil,
		// Discord markdown, brackets, and quotes around the trigger
		link + " **!gym**":       {"gym"},
		link + " *!gym*":         {"gym"},
		link + " __!gym__":       {"gym"},
		link + " ~~!gym~~":       {"gym"},
		link + " ||!gym||":       {"gym"},
		link + " `!gym`":         {"gym"},
		link + " (!gym, !chill)": {"gym", "chill"},
		link + " [!gym]":         {"gym"},
		link + ` "!gym"`:         {"gym"},
		link + " “!gym”":         {"gym"}, // iOS smart quotes
		"> !gym " + link:         {"gym"},
		// Unusual spaces from phones and copy-paste
		link + " !gym": {"gym"},
		link + "　!gym": {"gym"},
		// Trailing separators aren't part of the name
		link + " !gym-":           {"gym"},
		link + " !gym_":           {"gym"},
		link + " !chill--so good": {"chill"},
	}
	for content, want := range tests {
		assert.Equal(t, want, c.Extract(content), content)
	}
}

func TestResolve(t *testing.T) {
	c := newTestConfig(t)
	tests := []struct {
		name        string
		content     string
		wantIDs     []string
		wantUnknown []string
	}{
		{"link alone", link, []string{"default-id"}, nil},
		{"non-trigger words", link + " you guys gotta check out this song!", []string{"default-id"}, nil},
		{"one trigger skips default", link + " !gym", []string{"gym-id"}, nil},
		{"trigger with multiple playlists", link + " !relaxing", []string{"relax-id", "chill-id"}, nil},
		{"default plus trigger", link + " !default !gym", []string{"default-id", "gym-id"}, nil},
		{"shared playlist added once", link + " !relaxing !chill", []string{"relax-id", "chill-id"}, nil},
		{"unknown only falls back to default", link + " !nope", []string{"default-id"}, []string{"nope"}},
		{"unknown alongside known", link + " !nope !gym !what", []string{"gym-id"}, []string{"nope", "what"}},
		{"default keyword is case-insensitive", link + " !DEFAULT", []string{"default-id"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, unknown := c.Resolve(tt.content)
			assert.Equal(t, tt.wantIDs, ids)
			assert.Equal(t, tt.wantUnknown, unknown)
		})
	}
}

func TestCustomPrefix(t *testing.T) {
	c, err := New("$", "default-id", map[string][]string{"gym": {"gym-id"}}, false)
	require.NoError(t, err)
	ids, _ := c.Resolve(link + " $gym !gym")
	assert.Equal(t, []string{"gym-id"}, ids)
	ids, _ = c.Resolve(link + " that was $5")
	assert.Equal(t, []string{"default-id"}, ids)
}
