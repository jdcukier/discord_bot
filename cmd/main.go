// Package main provides the entry point for the Discord bots
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
	if err := run(); err != nil {
		logger.Fatal("Exiting", zap.Error(err))
	}
}

// run starts the service and blocks until the HTTP server stops
func run() error {
	defer func() {
		if err := logger.Sync(); err != nil {
			logger.Error("Failed to sync logger", zap.Error(err))
		}
	}()

	// Load and validate environment variables
	if err := loadAndValidateEnv(); err != nil {
		return err
	}

	// Start the HTTP server
	port := httputil.Port()
	logger.Info("Starting server", zap.String(zapkey.Port, port))
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- http.ListenAndServe(port, nil)
	}()
	// Give the server a moment to start, and fail fast if it couldn't
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-serverErr:
		return fmt.Errorf("failed to start server on %s: %w", port, err)
	default:
	}

	// Initialize clients
	var clients []Client

	// Initialize Debug client
	// Note: This must be initialized first since it hosts the root HTTP path
	debugClient, err := debug.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Debug client: %w", err)
	}
	clients = append(clients, debugClient)

	// Initialize Spotify client
	spotifyClient, err := spotify.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create Spotify client: %w", err)
	}

	// Initialize the Spotify Discord bot with the spotify client
	spotifyBot, err := newSpotifyBot(spotifyClient)
	if err != nil {
		return fmt.Errorf("failed to create Spotify Discord bot: %w", err)
	}
	clients = append(clients, spotifyBot)

	// Wire Discord health into the debug client's /health endpoint
	debugClient.AddHealthChecker(spotifyBot)

	// Update spotify client with discord messenger
	spotifyClient.SetMessenger(spotifyBot)
	clients = append(clients, spotifyClient)

	// Stop clients when the program exits
	defer func() {
		for _, client := range clients {
			if err := client.Stop(); err != nil {
				logger.Error("Failed to stop client", zap.Error(err), zap.Stringer(zapkey.Client, client))
			}
		}
	}()

	// Start clients
	for _, client := range clients {
		if err := client.Start(); err != nil {
			return fmt.Errorf("failed to start %s: %w", client, err)
		}
	}

	// Block until the HTTP server stops
	return fmt.Errorf("server stopped: %w", <-serverErr)
}

// --- Helpers ---

func loadAndValidateEnv() error {
	if err := godotenv.Load(); err != nil {
		logger.Info("No .env file found or unreadable; proceeding with system environment", zap.Error(err))
	}
	required := []string{
		envvar.Namespaced(envvar.DiscordToken, envvar.NamespaceSpotify),
		envvar.Namespaced(envvar.DiscordAppID, envvar.NamespaceSpotify),
		envvar.Namespaced(envvar.DiscordAuthChannelID, envvar.NamespaceSpotify),
		envvar.Namespaced(envvar.DiscordSongsChannelID, envvar.NamespaceSpotify),
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
		return fmt.Errorf("missing required environment variables: %v", missing)
	}
	return nil
}

// withVersion appends the bot version to a ready message
func withVersion(msg string) string {
	version := os.Getenv(envvar.BotVersion)
	if version == "" {
		version = "Unknown version"
	}
	return fmt.Sprintf("%s\nVersion: %s", msg, version)
}

// newSpotifyBot creates the Discord bot that collects songs into the Spotify playlist
func newSpotifyBot(playlistAdder discord.PlaylistAdder) (*discord.Client, error) {
	config, err := discordconfig.Load(envvar.NamespaceSpotify)
	if err != nil {
		return nil, err
	}
	if err := config.RequireChannels(discordchannel.Auth, discordchannel.Songs); err != nil {
		return nil, err
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

	readyMessage := config.ReadyMessage
	if readyMessage == "" {
		readyMessage = "Bot is online. Ready to record your songs."
	}
	listeningMessage := config.ListeningMessage
	if listeningMessage == "" {
		listeningMessage = "song requests"
	}
	songsChannelID := config.ChannelIDs[discordchannel.Songs]

	// Handlers
	handlers := []discord.Handler{
		discord.NewReadyHandler(songsChannelID, withVersion(readyMessage), listeningMessage),
		discord.NewMessageHandler(playlistAdder, actions),
		discord.NewInteractionSessionHandler(),
	}

	// Create the client
	return discord.NewClient(discord.WithConfig(config), discord.WithHandlers(handlers...))
}
