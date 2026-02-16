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
	AppID        = "DISCORD_APP_ID"
	DiscordToken = "DISCORD_TOKEN"
)

// Spotify-related constants
const (
	SpotifyID          = "SPOTIFY_ID"
	SpotifySecret      = "SPOTIFY_SECRET"
	SpotifyRedirectURI = "SPOTIFY_REDIRECT_URI"
	SpotifyPlaylistID  = "SPOTIFY_PLAYLIST_ID"
)
