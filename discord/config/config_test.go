package config

import (
	"errors"
	"testing"

	"discordbot/discord/channel"
)

func TestLoadReadsNamespacedEnvVars(t *testing.T) {
	t.Setenv("DISCORD_TOKEN_TEST", "token")
	t.Setenv("DISCORD_APP_ID_TEST", "app-id")
	t.Setenv("DISCORD_SONGS_CHANNEL_ID_TEST", "songs-id")
	// Un-namespaced and other-namespace vars must be ignored
	t.Setenv("DISCORD_TOKEN", "wrong-token")
	t.Setenv("DISCORD_AUTH_CHANNEL_ID_OTHER", "wrong-auth-id")

	c, err := Load("TEST")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Namespace != "TEST" || c.Token != "token" || c.AppID != "app-id" {
		t.Errorf("Load() = %+v, want namespace TEST, token and app-id from TEST vars", c)
	}
	if got := c.ChannelIDs[channel.Songs]; got != "songs-id" {
		t.Errorf("Songs channel ID = %q, want %q", got, "songs-id")
	}
	if _, ok := c.ChannelIDs[channel.Auth]; ok {
		t.Errorf("Auth channel ID set to %q, want unset", c.ChannelIDs[channel.Auth])
	}
}

func TestLoadNotConfigured(t *testing.T) {
	_, err := Load("MISSING")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load() error = %v, want ErrNotConfigured", err)
	}
}

func TestLoadRequiresAppID(t *testing.T) {
	t.Setenv("DISCORD_TOKEN_NOAPP", "token")

	_, err := Load("NOAPP")
	if err == nil || errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load() error = %v, want an invalid-config error", err)
	}
}

func TestRequireChannels(t *testing.T) {
	c := &Config{
		Namespace:  "TEST",
		ChannelIDs: map[channel.Type]string{channel.Songs: "songs-id"},
	}
	if err := c.RequireChannels(channel.Songs); err != nil {
		t.Errorf("RequireChannels(Songs) error = %v, want nil", err)
	}
	if err := c.RequireChannels(channel.Songs, channel.Auth); err == nil {
		t.Error("RequireChannels(Songs, Auth) error = nil, want missing Auth error")
	}
}
