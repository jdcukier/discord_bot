package trigger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gymID   = "0123456789abcdefghijkl"
	chillID = "ABCDEFGHIJKLMNOPQRSTUV"
)

func openTestStore(t *testing.T, path string, playlists map[string][]string) (*Store, bool) {
	t.Helper()
	settings, err := New(DefaultPrefix, "default-id", nil, false)
	require.NoError(t, err)
	store, imported, err := OpenStore(path, settings, playlists)
	require.NoError(t, err)
	return store, imported
}

func TestOpenStoreImportsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "tags.json")

	store, imported := openTestStore(t, path, map[string][]string{"gym": {gymID}})
	assert.True(t, imported)
	assert.Equal(t, map[string][]string{"gym": {gymID}}, store.Config().Playlists)

	// Once the file exists, the env tags are ignored
	store, imported = openTestStore(t, path, map[string][]string{"other": {chillID}})
	assert.False(t, imported)
	assert.Equal(t, map[string][]string{"gym": {gymID}}, store.Config().Playlists)
}

func TestOpenStoreEmptyImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	store, imported := openTestStore(t, path, nil)
	assert.True(t, imported)
	assert.Empty(t, store.Config().Playlists)

	store, imported = openTestStore(t, path, map[string][]string{"gym": {gymID}})
	assert.False(t, imported)
	assert.Empty(t, store.Config().Playlists)
}

func TestOpenStoreInvalidFile(t *testing.T) {
	base, err := New(DefaultPrefix, "default-id", nil, false)
	require.NoError(t, err)
	for _, content := range []string{
		`not json`,
		`{"tags": {"Bad Name": ["x"]}}`,
		`{"tags": {"default": ["x"]}}`,
		`{"tags": {"gym": ["not-an-id"]}}`,
	} {
		path := filepath.Join(t.TempDir(), "tags.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, _, err := OpenStore(path, base, nil)
		assert.Error(t, err, content)
	}
}

func TestOpenStoreNormalizes(t *testing.T) {
	// Imported links become bare IDs, so /tag remove can match them
	path := filepath.Join(t.TempDir(), "tags.json")
	store, _ := openTestStore(t, path, map[string][]string{
		"gym":   {"https://open.spotify.com/playlist/" + gymID + "?si=abc", gymID},
		"empty": {},
	})
	assert.Equal(t, map[string][]string{"gym": {gymID}}, store.Config().Playlists)
	_, err := store.Remove("gym", gymID)
	require.NoError(t, err)

	// Same for a hand-edited file
	path = filepath.Join(t.TempDir(), "tags.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"tags": {"Chill": ["spotify:playlist:`+chillID+`"], "none": []}}`), 0o600))
	_, _, err = OpenStore(path, store.Config(), nil)
	require.Error(t, err, "uppercase names are invalid in the file")
	require.NoError(t, os.WriteFile(path, []byte(`{"tags": {"chill": ["spotify:playlist:`+chillID+`"], "none": []}}`), 0o600))
	reopened, _, err := OpenStore(path, store.Config(), nil)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"chill": {chillID}}, reopened.Config().Playlists)
}

func TestOpenStoreInvalidSeed(t *testing.T) {
	settings, err := New(DefaultPrefix, "default-id", nil, false)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "tags.json")
	_, _, err = OpenStore(path, settings, map[string][]string{"gym": {"https://open.spotify.com/track/" + gymID}})
	require.Error(t, err)
	assert.NoFileExists(t, path)
}

func TestStoreAddRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	store, _ := openTestStore(t, path, nil)

	ids, added, err := store.Add("gym", gymID)
	require.NoError(t, err)
	assert.True(t, added)
	assert.Equal(t, []string{gymID}, ids)

	ids, added, err = store.Add("gym", chillID)
	require.NoError(t, err)
	assert.True(t, added)
	assert.Equal(t, []string{gymID, chillID}, ids)

	_, added, err = store.Add("gym", gymID)
	require.NoError(t, err)
	assert.False(t, added)

	// Changes apply to message routing immediately
	got, _ := store.Config().Resolve("link !gym")
	assert.Equal(t, []string{gymID, chillID}, got)

	// Changes survive a restart
	reopened, imported := openTestStore(t, path, nil)
	assert.False(t, imported)
	assert.Equal(t, store.Config().Playlists, reopened.Config().Playlists)

	remaining, err := store.Remove("gym", gymID)
	require.NoError(t, err)
	assert.Equal(t, []string{chillID}, remaining)

	_, err = store.Remove("gym", gymID)
	assert.Error(t, err, "playlist no longer in tag")

	// Removing the last playlist deletes the tag
	remaining, err = store.Remove("gym", chillID)
	require.NoError(t, err)
	assert.Empty(t, remaining)
	assert.NotContains(t, store.Config().Playlists, "gym")

	_, _, err = store.Add("chill", chillID)
	require.NoError(t, err)
	remaining, err = store.Remove("chill", "")
	require.NoError(t, err)
	assert.Empty(t, remaining)
	assert.Empty(t, store.Config().Playlists)

	_, err = store.Remove("chill", "")
	assert.Error(t, err, "unknown tag")
}

func TestStoreAddErrors(t *testing.T) {
	store, _ := openTestStore(t, filepath.Join(t.TempDir(), "tags.json"), nil)
	for _, tc := range []struct{ name, id string }{
		{"default", gymID},
		{"Gym", gymID},
		{"two words", gymID},
		{"gym-", gymID},
		{"gym", "short"},
	} {
		_, _, err := store.Add(tc.name, tc.id)
		assert.Error(t, err, tc)
	}
	assert.Empty(t, store.Config().Playlists)
}

func TestParsePlaylistID(t *testing.T) {
	for input, want := range map[string]string{
		gymID: gymID,
		"https://open.spotify.com/playlist/" + gymID + "?si=abc": gymID,
		"https://open.spotify.com/intl-fr/playlist/" + gymID:     gymID,
		"  <https://open.spotify.com/playlist/" + gymID + ">  ":  gymID,
		"spotify:playlist:" + gymID:                              gymID,
	} {
		got, err := ParsePlaylistID(input)
		require.NoError(t, err, input)
		assert.Equal(t, want, got, input)
	}

	for _, input := range []string{
		"",
		"short",
		"https://open.spotify.com/track/" + gymID,
		"https://open.spotify.com/album/" + gymID,
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", // Spotify editorial
	} {
		_, err := ParsePlaylistID(input)
		assert.Error(t, err, input)
	}
}
