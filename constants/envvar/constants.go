// Package envvar defines environment variable keys as constants
package envvar

// General constants
const (
	VerboseLogsEnabled = "VERBOSE_LOGS_ENABLED"
)

// HTTP-related constants
const (
	Port = "PORT"
)

// Discord-related constants
const (
	// Authentication
	DiscordAppID = "DISCORD_APP_ID"
	DiscordToken = "DISCORD_TOKEN"

	// Channel IDs
	DiscordAuthChannelID  = "DISCORD_AUTH_CHANNEL_ID"
	DiscordDebugChannelID = "DISCORD_DEBUG_CHANNEL_ID"
	DiscordSongsChannelID = "DISCORD_SONGS_CHANNEL_ID"
)

// Spotify-related constants
const (
	SpotifyAppID       = "SPOTIFY_APP_ID"
	SpotifySecret      = "SPOTIFY_SECRET"
	SpotifyRedirectURI = "SPOTIFY_REDIRECT_URI"
	SpotifyPlaylistID  = "SPOTIFY_PLAYLIST_ID"
)
