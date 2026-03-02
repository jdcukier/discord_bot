// Package main provides the entry point for the Discord bot
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/debug"
	"discordbot/discord"
	discordchannel "discordbot/discord/channel"
	discordconfig "discordbot/discord/config"
	"discordbot/spotify"
	"discordbot/utils/httputil"
)

type Client interface {
	fmt.Stringer
	Start() error
	Stop() error
}

// main entry point for the application
func main() {
	// Initialize logger first
	defer func() {
		if err := logger.Sync(); err != nil {
			logger.Error("Failed to sync logger", zap.Error(err))
		}
	}()

	// Load and validate environment variables
	loadAndValidateEnv()

	// Start the HTTP server
	port := httputil.Port()
	logger.Info("Starting server", zap.String(zapkey.Port, port))
	go func() {
		if err := http.ListenAndServe(port, nil); err != nil {
			logger.Fatal("Failed to start server", zap.Error(err), zap.String(zapkey.Port, port))
		}
	}()
	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Initialize clients
	var clients []Client

	// Initialize Debug client
	// Note: This must be initialized first since it hosts the root HTTP path
	debugClient, err := debug.NewClient()
	if err != nil {
		logger.Fatal("Failed to create Debug client", zap.Error(err))
	}
	clients = append(clients, debugClient)

	// Initialize Spotify client
	spotifyClient, err := spotify.NewClient()
	if err != nil {
		logger.Fatal("Failed to create Spotify client", zap.Error(err))
	}

	// Initialize Discord client with the spotify client
	discordClient := newDiscordClient(spotifyClient, readyMessage())
	clients = append(clients, discordClient)

	// Wire Discord health into the debug client's /health endpoint
	debugClient.SetHealthChecker(discordClient)

	// Update spotify client with discord messenger
	spotifyClient.SetMessenger(discordClient)
	clients = append(clients, spotifyClient)

	// Start clients
	for _, client := range clients {
		if err := client.Start(); err != nil {
			logger.Fatal("Failed to start client", zap.Error(err), zap.Stringer(zapkey.Client, client))
		}
	}

	// Stop clients when the program exits
	defer func() {
		for _, client := range clients {
			if err := client.Stop(); err != nil {
				logger.Error("Failed to stop client", zap.Error(err), zap.Stringer(zapkey.Client, client))
			}
		}
	}()

	// Wait for server shutdown (this will block forever)
	select {}
}

// --- Helpers ---

func loadAndValidateEnv() {
	if err := godotenv.Load(); err != nil {
		logger.Info("No .env file found or unreadable; proceeding with system environment", zap.Error(err))
	}
	required := []string{
		envvar.DiscordToken,
		envvar.DiscordAppID,
		envvar.DiscordAuthChannelID,
		envvar.DiscordSongsChannelID,
		envvar.SpotifyPlaylistID,
		envvar.SpotifyWorkerURL,
		envvar.CFAccessClientID,
		envvar.CFAccessClientSecret,
	}
	var missing []string
	for _, v := range required {
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		logger.Fatal("missing required environment variables",
			zap.Strings("vars", missing))
	}
}

func listeningActivity() string {
	msg := os.Getenv(envvar.BotListeningMessage)
	if msg == "" {
		msg = "song requests"
	}
	return msg
}

func readyMessage() string {
	msg := os.Getenv(envvar.BotReadyMessage)
	if msg == "" {
		msg = "Bot is online. Ready to record your songs."
	}
	version := os.Getenv(envvar.BotVersion)
	if version == "" {
		version = "Unknown version"
	}
	return fmt.Sprintf("%s\nVersion: %s", msg, version)
}

func newDiscordClient(playlistAdder discord.PlaylistAdder, botReadyMessage string) *discord.Client {
	config, err := discordconfig.NewConfig()
	if err != nil {
		logger.Fatal("Failed to create Discord config", zap.Error(err))
	}

	// Actions to perform when a message is received
	actions := make(discord.ChannelActions)
	for channelType, channelID := range config.ChannelIDs {
		switch channelType {
		case discordchannel.Songs:
			// Add tracks to playlist for the Songs channel
			actions.Add(channelID, discord.ActionAddTracksToPlaylist)
		case discordchannel.Debug:
			// Add tracks to playlist and send a reply for the Debug channel
			actions.Add(channelID, discord.ActionReply)
			actions.Add(channelID, discord.ActionAddTracksToPlaylist)
		default:
			logger.Warn("Skipping unknown channel type",
				zap.String(zapkey.ChannelType, channelType.String()),
				zap.String(zapkey.ChannelID, channelID))
		}
	}

	songsChannelID := config.ChannelIDs[discordchannel.Songs]

	// Handlers
	handlers := []discord.Handler{
		discord.NewReadyHandler(songsChannelID, botReadyMessage, listeningActivity()),
		discord.NewMessageHandler(playlistAdder, actions),
		discord.NewInteractionSessionHandler(),
	}

	// Create the client
	discordClient, err := discord.NewClient(discord.WithHandlers(handlers...))
	if err != nil {
		logger.Fatal("Failed to create Discord client", zap.Error(err))
	}
	return discordClient
}
