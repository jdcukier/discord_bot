package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/discord/channel"
)

func TestLoadReadsNamespacedEnvVars(t *testing.T) {
	t.Setenv("DISCORD_TOKEN_TEST", "token")
	t.Setenv("DISCORD_APP_ID_TEST", "app-id")
	t.Setenv("DISCORD_SONGS_CHANNEL_ID_TEST", "songs-id")
	t.Setenv("BOT_READY_MESSAGE_TEST", "ready")
	t.Setenv("BOT_LISTENING_MESSAGE_TEST", "listening")
	// Un-namespaced and other-namespace vars must be ignored
	t.Setenv("DISCORD_TOKEN", "wrong-token")
	t.Setenv("DISCORD_AUTH_CHANNEL_ID_OTHER", "wrong-auth-id")
	t.Setenv("BOT_READY_MESSAGE", "wrong-ready")

	c, err := Load("TEST")
	require.NoError(t, err)

	assert.Equal(t, "TEST", c.Namespace)
	assert.Equal(t, "token", c.Token)
	assert.Equal(t, "app-id", c.AppID)
	assert.Equal(t, "ready", c.ReadyMessage)
	assert.Equal(t, "listening", c.ListeningMessage)
	assert.Equal(t, map[channel.Type]string{channel.Songs: "songs-id"}, c.ChannelIDs)
}

func TestLoadOptionalMessagesDefaultEmpty(t *testing.T) {
	t.Setenv("DISCORD_TOKEN_TEST", "token")
	t.Setenv("DISCORD_APP_ID_TEST", "app-id")

	c, err := Load("TEST")
	require.NoError(t, err)

	assert.Empty(t, c.ReadyMessage)
	assert.Empty(t, c.ListeningMessage)
	assert.Empty(t, c.ChannelIDs)
}

func TestLoadNotConfigured(t *testing.T) {
	_, err := Load("MISSING")

	assert.ErrorIs(t, err, ErrNotConfigured)
}

func TestLoadRequiresAppID(t *testing.T) {
	t.Setenv("DISCORD_TOKEN_NOAPP", "token")

	_, err := Load("NOAPP")

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotConfigured)
}

func TestRequireChannels(t *testing.T) {
	c := &Config{
		Namespace:  "TEST",
		ChannelIDs: map[channel.Type]string{channel.Songs: "songs-id"},
	}

	assert.NoError(t, c.RequireChannels(channel.Songs))
	assert.ErrorContains(t, c.RequireChannels(channel.Songs, channel.Auth), "DISCORD_AUTH_CHANNEL_ID_TEST")
}
