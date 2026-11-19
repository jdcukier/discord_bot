package trigger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gymID     = "0123456789abcdefghijkl"
	chillID   = "ABCDEFGHIJKLMNOPQRSTUV"
	defaultID = "DEFAULT000000000000000"
)

func testSettings(t *testing.T) *Config {
	t.Helper()
	settings, err := New(DefaultPrefix, defaultID, nil, false)
	require.NoError(t, err)
	return settings
}

func openTestStore(t *testing.T, path string, seed map[string][]string) (*Store, OpenResult) {
	t.Helper()
	store, result, err := OpenStore(path, testSettings(t), seed)
	require.NoError(t, err)
	return store, result
}

func TestOpenStoreImportsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "tags.json")

	store, result := openTestStore(t, path, map[string][]string{"gym": {gymID}})
	assert.True(t, result.Created)
	assert.Empty(t, result.Changed)
	assert.Equal(t, map[string]string{"gym": gymID}, store.Config().Tags)

	// Once the file exists, the seed is ignored
	store, result = openTestStore(t, path, map[string][]string{"other": {chillID}})
	assert.False(t, result.Created)
	assert.Equal(t, map[string]string{"gym": gymID}, store.Config().Tags)
}

func TestOpenStoreEmptyImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	store, result := openTestStore(t, path, nil)
	assert.True(t, result.Created)
	assert.Empty(t, store.Config().Tags)

	store, result = openTestStore(t, path, map[string][]string{"gym": {gymID}})
	assert.False(t, result.Created)
	assert.Empty(t, store.Config().Tags)
}

func TestOpenStoreConvertsSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	store, result := openTestStore(t, path, map[string][]string{
		"relax":  {"https://open.spotify.com/playlist/" + chillID + "?si=abc"},
		"rehlax": {defaultID, chillID},
		"empty":  {},
	})
	assert.True(t, result.Created)
	assert.Equal(t, []string{"rehlax"}, result.Changed)
	assert.Equal(t, map[string]string{"relax": chillID, "rehlax": chillID}, store.Config().Tags)
}

func TestOpenStoreConvertsLegacyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	legacy := `{"tags": {"relax": ["` + chillID + `"], "rehlax": ["` + defaultID + `", "spotify:playlist:` + chillID + `"], "none": []}}`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

	store, result := openTestStore(t, path, nil)
	assert.False(t, result.Created)
	assert.Equal(t, []string{"rehlax"}, result.Changed)
	assert.Equal(t, map[string]string{"relax": chillID, "rehlax": chillID}, store.Config().Tags)

	// Saved in the current format, so it's only converted once
	reopened, result := openTestStore(t, path, nil)
	assert.Empty(t, result.Changed)
	assert.Equal(t, store.Config().Tags, reopened.Config().Tags)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"relax": "`+chillID+`"`)
}

func TestOpenStoreInvalidFile(t *testing.T) {
	for _, content := range []string{
		`not json`,
		`{"tags": {"Bad Name": "` + gymID + `"}}`,
		`{"tags": {"default": "` + gymID + `"}}`,
		`{"tags": {"gym": "not-an-id"}}`,
		`{"tags": {"gym": ["not-an-id"]}}`,
		`{"tags": {"gym": 5}}`,
	} {
		path := filepath.Join(t.TempDir(), "tags.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, _, err := OpenStore(path, testSettings(t), nil)
		assert.Error(t, err, content)
	}
}

func TestOpenStoreInvalidSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	_, _, err := OpenStore(path, testSettings(t), map[string][]string{"gym": {"https://open.spotify.com/track/" + gymID}})
	require.Error(t, err)
	assert.NoFileExists(t, path)
}

func TestStoreSetRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.json")
	store, _ := openTestStore(t, path, nil)

	previous, err := store.Set("gym", gymID)
	require.NoError(t, err)
	assert.Empty(t, previous)

	// Setting again replaces the playlist
	previous, err = store.Set("gym", chillID)
	require.NoError(t, err)
	assert.Equal(t, gymID, previous)

	previous, err = store.Set("gym", chillID)
	require.NoError(t, err)
	assert.Equal(t, chillID, previous)

	// Changes apply to message routing immediately
	got, _ := store.Config().Resolve("link +gym")
	assert.Equal(t, []string{chillID, defaultID}, got)

	// Changes survive a restart
	reopened, result := openTestStore(t, path, nil)
	assert.False(t, result.Created)
	assert.Equal(t, store.Config().Tags, reopened.Config().Tags)

	previous, err = store.Remove("gym")
	require.NoError(t, err)
	assert.Equal(t, chillID, previous)
	assert.Empty(t, store.Config().Tags)

	_, err = store.Remove("gym")
	assert.Error(t, err, "unknown tag")
}

func TestStoreAdd(t *testing.T) {
	store, _ := openTestStore(t, filepath.Join(t.TempDir(), "tags.json"), nil)

	existing, err := store.Add("gym", gymID)
	require.NoError(t, err)
	assert.Empty(t, existing)

	// Add never replaces a tag's playlist
	existing, err = store.Add("gym", chillID)
	require.NoError(t, err)
	assert.Equal(t, gymID, existing)

	// Several tags can share a playlist
	existing, err = store.Add("workout", gymID)
	require.NoError(t, err)
	assert.Empty(t, existing)
	assert.Equal(t, map[string]string{"gym": gymID, "workout": gymID}, store.Config().Tags)

	_, err = store.Add("default", gymID)
	assert.Error(t, err)
}

func TestStoreSetErrors(t *testing.T) {
	store, _ := openTestStore(t, filepath.Join(t.TempDir(), "tags.json"), nil)
	for _, tc := range []struct{ name, id string }{
		{"default", gymID},
		{"Gym", gymID},
		{"two words", gymID},
		{"gym-", gymID},
		{"gym", "short"},
	} {
		_, err := store.Set(tc.name, tc.id)
		assert.Error(t, err, tc)
	}
	assert.Empty(t, store.Config().Tags)
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
