// Package envvar defines environment variable keys as constants
package envvar

// General constants
const (
	VerboseLogsEnabled  = "VERBOSE_LOGS_ENABLED"
	BotVersion          = "BOT_VERSION"
	BotReadyMessage     = "BOT_READY_MESSAGE"
	BotListeningMessage = "BOT_LISTENING_MESSAGE"
)

// HTTP-related constants
const (
	Port = "PORT"
)

// Discord-related constants
// These are per-bot: read them with Namespaced, e.g. DISCORD_TOKEN_SPOTIFY
const (
	// Authentication
	DiscordAppID = "DISCORD_APP_ID"
	DiscordToken = "DISCORD_TOKEN"

	// Channel IDs
	DiscordAuthChannelID  = "DISCORD_AUTH_CHANNEL_ID"
	DiscordDebugChannelID = "DISCORD_DEBUG_CHANNEL_ID"
	DiscordSongsChannelID = "DISCORD_SONGS_CHANNEL_ID"
)

// Bot namespaces used to suffix per-bot env vars
const (
	NamespaceSpotify = "SPOTIFY"
	NamespaceArcade  = "ARCADE"
)

// Namespaced returns the per-bot form of an env var key, e.g. DISCORD_TOKEN_SPOTIFY
func Namespaced(key, namespace string) string {
	return key + "_" + namespace
}

// Spotify-related constants
const (
	SpotifyPlaylistID = "SPOTIFY_PLAYLIST_ID"
	SpotifyWorkerURL  = "SPOTIFY_WORKER_URL"
)

// Cloudflare worker access
const (
	CFAccessClientID     = "CF_ACCESS_CLIENT_ID"
	CFAccessClientSecret = "CF_ACCESS_CLIENT_SECRET"
)
