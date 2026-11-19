package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/constants/envvar"
)

const testPlaylistID = "0123456789abcdefghijkl"

func setTagEnv(t *testing.T, triggers string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tags.yaml")
	t.Setenv(envvar.SpotifyTagsFile, path)
	t.Setenv(envvar.SpotifyPlaylistID, "default-id")
	t.Setenv(envvar.SpotifyPlaylistTriggers, triggers)
	t.Setenv(envvar.SpotifyTriggerPrefix, "")
	t.Setenv(envvar.SpotifyReplyUnknownTriggers, "")
	return path
}

func TestLoadPlaylistTagsImportsSecret(t *testing.T) {
	path := setTagEnv(t, "gym="+testPlaylistID)
	triggers, tags := loadPlaylistTags()
	require.NotNil(t, triggers)
	require.NotNil(t, tags)
	assert.Equal(t, map[string]string{"gym": testPlaylistID}, triggers.Config().Tags)
	assert.FileExists(t, path)
}

func TestLoadPlaylistTagsIgnoresInvalidSecretOnceFileExists(t *testing.T) {
	path := setTagEnv(t, "gym="+testPlaylistID)
	_, tags := loadPlaylistTags()
	require.NotNil(t, tags)

	t.Setenv(envvar.SpotifyPlaylistTriggers, "not valid")
	triggers, tags := loadPlaylistTags()
	require.NotNil(t, triggers)
	require.NotNil(t, tags, "/tag stays enabled")
	assert.Equal(t, map[string]string{"gym": testPlaylistID}, triggers.Config().Tags)
	assert.FileExists(t, path)
}

func TestLoadPlaylistTagsInvalidSecretWithoutFile(t *testing.T) {
	path := setTagEnv(t, "not valid")
	triggers, tags := loadPlaylistTags()
	assert.Nil(t, triggers)
	assert.Nil(t, tags)
	// No empty file that would hide the tags once the secret is fixed
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadPlaylistTagsConvertsJSONFile(t *testing.T) {
	path := setTagEnv(t, "not valid")
	jsonPath := filepath.Join(filepath.Dir(path), "tags.json")
	require.NoError(t, os.WriteFile(jsonPath, []byte(`{"tags": {"gym": "`+testPlaylistID+`"}}`), 0o600))

	// The invalid secret is ignored, since there are tags to convert
	triggers, tags := loadPlaylistTags()
	require.NotNil(t, triggers)
	require.NotNil(t, tags, "/tag stays enabled")
	assert.Equal(t, map[string]string{"gym": testPlaylistID}, triggers.Config().Tags)
	assert.FileExists(t, path)
	assert.FileExists(t, jsonPath+".bak")
	assert.NoFileExists(t, jsonPath)
}
