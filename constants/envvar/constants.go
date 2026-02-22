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
	DiscordAppID         = "DISCORD_APP_ID"
	DiscordAuthChannelID = "DISCORD_AUTH_CHANNEL_ID"
	DiscordToken         = "DISCORD_TOKEN"
)

// Spotify-related constants
const (
	SpotifyAppID       = "SPOTIFY_APP_ID"
	SpotifySecret      = "SPOTIFY_SECRET"
	SpotifyRedirectURI = "SPOTIFY_REDIRECT_URI"
	SpotifyPlaylistID  = "SPOTIFY_PLAYLIST_ID"
)
