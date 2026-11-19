package trigger

import (
	"os"
	"path/filepath"
	"strings"
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestHeaderIsAligned(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(Header, "\n"), "\n")
	for _, line := range lines {
		assert.True(t, strings.HasPrefix(line, "# "), line)
		assert.Equal(t, len([]rune(lines[0])), len([]rune(line)), line)
	}
}

func TestLegacyPath(t *testing.T) {
	assert.Equal(t, "/data/playlist-tags.json", LegacyPath("/data/playlist-tags.yaml"))
	assert.Equal(t, "tags.json", LegacyPath("tags.yml"))
	assert.Equal(t, "/data/tags.json", LegacyPath("/data/tags"))
}

func TestOpenStoreImportsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "tags.yaml")

	store, result := openTestStore(t, path, map[string][]string{"gym": {gymID}})
	assert.True(t, result.Created)
	assert.Empty(t, result.Dropped)
	assert.Equal(t, map[string]string{"gym": gymID}, store.Config().Tags)
	assert.Equal(t, Header+"playlists:\n  - id: "+gymID+"\n    tags:\n      - gym\n", readFile(t, path))

	// Once the file exists, the seed is ignored
	store, result = openTestStore(t, path, map[string][]string{"other": {chillID}})
	assert.False(t, result.Created)
	assert.Equal(t, map[string]string{"gym": gymID}, store.Config().Tags)
}

func TestOpenStoreEmptyImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
	store, result := openTestStore(t, path, nil)
	assert.True(t, result.Created)
	assert.Empty(t, store.Config().Tags)
	assert.Equal(t, Header+"playlists: []\n", readFile(t, path))

	store, result = openTestStore(t, path, map[string][]string{"gym": {gymID}})
	assert.False(t, result.Created)
	assert.Empty(t, store.Config().Tags)
}

func TestOpenStoreConvertsSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
	store, result := openTestStore(t, path, map[string][]string{
		"relax":  {"https://open.spotify.com/playlist/" + chillID + "?si=abc"},
		"rehlax": {defaultID, chillID},
		"empty":  {},
	})
	assert.True(t, result.Created)
	assert.Equal(t, map[string][]string{"rehlax": {defaultID}}, result.Dropped)
	assert.Equal(t, map[string]string{"relax": chillID, "rehlax": chillID}, store.Config().Tags)
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
	store, _ := openTestStore(t, path, map[string][]string{
		"workout": {gymID},
		"gym":     {gymID},
		"chill":   {chillID},
		"main":    {defaultID},
	})
	require.NoError(t, store.SetPlaylistName(gymID, "Gym Bangers"))
	require.NoError(t, store.SetPlaylistName(chillID, "Late Night: \"chill\" # vibes"))

	// Sorted by name, then ID, so the unnamed default playlist comes first; tags are sorted too
	want := Header + `playlists:
  - id: ` + defaultID + `
    tags:
      - main
  - name: Gym Bangers
    id: ` + gymID + `
    tags:
      - gym
      - workout
  - name: 'Late Night: "chill" # vibes'
    id: ` + chillID + `
    tags:
      - chill
`
	assert.Equal(t, want, readFile(t, path))

	reopened, result := openTestStore(t, path, nil)
	assert.False(t, result.Created)
	assert.Equal(t, store.Config().Tags, reopened.Config().Tags)
	assert.Equal(t, "Gym Bangers", reopened.PlaylistName(gymID))
	assert.Equal(t, `Late Night: "chill" # vibes`, reopened.PlaylistName(chillID))
	assert.Empty(t, reopened.PlaylistName(defaultID))

	// Saving again writes the same file
	_, err := reopened.Set("extra", gymID)
	require.NoError(t, err)
	_, err = reopened.Remove("extra")
	require.NoError(t, err)
	assert.Equal(t, want, readFile(t, path))
}

func TestOpenStoreReadsHandEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
	content := `# My comment
playlists:
  - id: https://open.spotify.com/playlist/` + gymID + `?si=x
    name: Gym
    tags: [gym, workout]
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	store, _ := openTestStore(t, path, nil)
	assert.Equal(t, map[string]string{"gym": gymID, "workout": gymID}, store.Config().Tags)
	assert.Equal(t, "Gym", store.PlaylistName(gymID))

	// An empty file has no tags
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	store, _ = openTestStore(t, path, nil)
	assert.Empty(t, store.Config().Tags)
}

func TestOpenStoreConvertsJSONFile(t *testing.T) {
	for name, tc := range map[string]struct {
		json    string
		dropped map[string][]string
	}{
		"one playlist per tag": {
			json: `{"tags": {"relax": "` + chillID + `", "rehlax": "spotify:playlist:` + chillID + `"}}`,
		},
		"several playlists per tag": {
			json:    `{"tags": {"relax": ["` + chillID + `"], "rehlax": ["` + defaultID + `", "spotify:playlist:` + chillID + `"], "none": []}}`,
			dropped: map[string][]string{"rehlax": {defaultID}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "tags.yaml")
			jsonPath := filepath.Join(dir, "tags.json")
			require.NoError(t, os.WriteFile(jsonPath, []byte(tc.json), 0o600))

			// The seed is ignored since there are tags to convert
			store, result := openTestStore(t, path, map[string][]string{"seed": {gymID}})
			assert.False(t, result.Created)
			if tc.dropped == nil {
				assert.Empty(t, result.Dropped)
			} else {
				assert.Equal(t, tc.dropped, result.Dropped)
			}
			assert.Equal(t, map[string]string{"relax": chillID, "rehlax": chillID}, store.Config().Tags)
			require.NoError(t, result.SaveErr)

			// Saved as YAML, with the JSON file moved aside
			assert.Contains(t, readFile(t, path), "tags:\n      - rehlax\n      - relax\n")
			assert.Equal(t, jsonPath+BackupSuffix, result.Backup)
			assert.Equal(t, tc.json, readFile(t, jsonPath+BackupSuffix))
			assert.NoFileExists(t, jsonPath)

			// So it's only converted once
			reopened, result := openTestStore(t, path, nil)
			assert.Empty(t, result.Dropped)
			assert.Empty(t, result.Backup)
			assert.Equal(t, store.Config().Tags, reopened.Config().Tags)
		})
	}
}

func TestOpenStorePrefersYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tags.yaml")
	jsonPath := filepath.Join(dir, "tags.json")
	require.NoError(t, os.WriteFile(path, []byte("playlists:\n  - id: "+gymID+"\n    tags: [gym]\n"), 0o600))
	require.NoError(t, os.WriteFile(jsonPath, []byte(`{"tags": {"relax": "`+chillID+`"}}`), 0o600))

	store, result := openTestStore(t, path, nil)
	assert.Empty(t, result.Backup)
	assert.Equal(t, map[string]string{"gym": gymID}, store.Config().Tags)
	assert.FileExists(t, jsonPath, "left alone")
}

func TestOpenStoreConvertsReadOnlyJSONFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tags.yaml")
	jsonPath := filepath.Join(dir, "tags.json")
	legacy := `{"tags": {"relax": ["` + chillID + `"]}}`
	require.NoError(t, os.WriteFile(jsonPath, []byte(legacy), 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	// The converted tags are used even though they can't be saved
	store, result := openTestStore(t, path, nil)
	require.Error(t, result.SaveErr)
	assert.Empty(t, result.Backup, "nothing was backed up yet")
	assert.Equal(t, map[string]string{"relax": chillID}, store.Config().Tags)
	assert.Equal(t, legacy, readFile(t, jsonPath), "the original is untouched")
	assert.NoFileExists(t, path)

	// Saving is retried on the next change, then the original is moved aside
	require.NoError(t, os.Chmod(dir, 0o700))
	_, err := store.Set("gym", gymID)
	require.NoError(t, err)
	assert.Equal(t, legacy, readFile(t, jsonPath+BackupSuffix))
	assert.NoFileExists(t, jsonPath)
	reopened, result := openTestStore(t, path, nil)
	assert.Empty(t, result.Backup)
	assert.Equal(t, map[string]string{"relax": chillID, "gym": gymID}, reopened.Config().Tags)
}

func TestOpenStoreCreateFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	store, result, err := OpenStore(filepath.Join(dir, "tags.yaml"), testSettings(t), map[string][]string{"gym": {gymID}})
	require.Error(t, err)
	assert.Nil(t, store)
	assert.Equal(t, OpenResult{}, result)
}

func TestOpenStoreKeepsEarlierBackups(t *testing.T) {
	// Upgrade, roll back by restoring the backup, change tags, upgrade again
	dir := t.TempDir()
	path := filepath.Join(dir, "tags.yaml")
	jsonPath := filepath.Join(dir, "tags.json")
	first := `{"tags": {"relax": "` + chillID + `"}}`
	second := `{"tags": {"relax": "` + chillID + `", "gym": "` + gymID + `"}}`
	require.NoError(t, os.WriteFile(jsonPath, []byte(first), 0o600))
	_, result := openTestStore(t, path, nil)
	assert.Equal(t, jsonPath+BackupSuffix, result.Backup)

	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(jsonPath, []byte(second), 0o600))
	store, result := openTestStore(t, path, nil)
	require.NoError(t, result.SaveErr)
	require.NotEqual(t, jsonPath+BackupSuffix, result.Backup)
	assert.True(t, strings.HasSuffix(result.Backup, BackupSuffix))
	assert.Len(t, store.Config().Tags, 2)

	assert.Equal(t, first, readFile(t, jsonPath+BackupSuffix))
	assert.Equal(t, second, readFile(t, result.Backup))
}

func TestOpenStoreInvalidJSONFile(t *testing.T) {
	for _, content := range []string{
		`not json`,
		`{"tags": {"Bad Name": "` + gymID + `"}}`,
		`{"tags": {"default": "` + gymID + `"}}`,
		`{"tags": {"gym": "not-an-id"}}`,
		`{"tags": {"gym": ["not-an-id"]}}`,
		`{"tags": {"gym": 5}}`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "tags.yaml")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "tags.json"), []byte(content), 0o600))
		_, _, err := OpenStore(path, testSettings(t), map[string][]string{"gym": {gymID}})
		assert.Error(t, err, content)
		assert.NoFileExists(t, path, "not created from the seed: it would hide the tags")
	}
}

func TestOpenStoreInvalidYAMLFile(t *testing.T) {
	entry := func(id string, tags ...string) string {
		return "  - id: " + id + "\n    tags: [" + strings.Join(tags, ", ") + "]\n"
	}
	for name, tc := range map[string]struct{ content, err string }{
		"not YAML":         {"playlists: [", "failed to parse YAML"},
		"wrong shape":      {"playlists:\n  gym: " + gymID + "\n", "failed to parse YAML"},
		"JSON format":      {`{"tags": {"gym": "` + gymID + `"}}`, "field tags not found"},
		"unknown field":    {"playlists:\n  - id: " + gymID + "\n    tag: [gym]\n", "field tag not found"},
		"invalid ID":       {"playlists:\n" + entry("not-an-id", "gym"), "isn't a valid Spotify playlist ID"},
		"track link":       {"playlists:\n" + entry("https://open.spotify.com/track/"+gymID, "gym"), "isn't a valid Spotify playlist ID"},
		"invalid tag":      {"playlists:\n" + entry(gymID, "Bad"), "invalid tag name"},
		"default tag":      {"playlists:\n" + entry(gymID, "default"), "reserved"},
		"empty tags":       {"playlists:\n" + entry(gymID), "has no tags"},
		"missing tags":     {"playlists:\n  - id: " + gymID + "\n", "has no tags"},
		"duplicate ID":     {"playlists:\n" + entry(gymID, "gym") + entry("spotify:playlist:"+gymID, "workout"), "listed more than once"},
		"tag under two":    {"playlists:\n" + entry(gymID, "gym") + entry(chillID, "chill", "gym"), "more than one playlist"},
		"tag listed twice": {"playlists:\n" + entry(gymID, "gym", "gym"), "listed twice"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tags.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			_, _, err := OpenStore(path, testSettings(t), map[string][]string{"seed": {chillID}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.err)
			assert.NotContains(t, err.Error(), gymID, "IDs are logged, so they're shortened")
			assert.Equal(t, tc.content, readFile(t, path), "left untouched")
		})
	}
}

func TestOpenStoreInvalidSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
	_, _, err := OpenStore(path, testSettings(t), map[string][]string{"gym": {"https://open.spotify.com/track/" + gymID}})
	require.Error(t, err)
	assert.NoFileExists(t, path)
}

func TestStorePlaylistNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
	store, _ := openTestStore(t, path, map[string][]string{"gym": {gymID}, "workout": {gymID}})
	assert.Empty(t, store.PlaylistName(gymID))

	require.NoError(t, store.SetPlaylistName(gymID, "Gym"))
	assert.Equal(t, "Gym", store.PlaylistName(gymID))
	assert.Contains(t, readFile(t, path), "name: Gym\n")

	// Empty names don't erase known ones
	require.NoError(t, store.SetPlaylistName(gymID, ""))
	assert.Equal(t, "Gym", store.PlaylistName(gymID))

	// Only tagged playlists have names
	require.NoError(t, store.SetPlaylistName(chillID, "Chill"))
	assert.Empty(t, store.PlaylistName(chillID))
	assert.NotContains(t, readFile(t, path), "Chill")

	// Tag changes keep the names of playlists that still have tags
	_, err := store.Remove("workout")
	require.NoError(t, err)
	assert.Equal(t, "Gym", store.PlaylistName(gymID))

	// And forget the others
	_, err = store.Set("gym", chillID)
	require.NoError(t, err)
	assert.Empty(t, store.PlaylistName(gymID))
	_, err = store.Set("gym", gymID)
	require.NoError(t, err)
	assert.Empty(t, store.PlaylistName(gymID))
	assert.NotContains(t, readFile(t, path), "name:")
}

func TestStoreSetRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tags.yaml")
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
	store, _ := openTestStore(t, filepath.Join(t.TempDir(), "tags.yaml"), nil)

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
	store, _ := openTestStore(t, filepath.Join(t.TempDir(), "tags.yaml"), nil)
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
