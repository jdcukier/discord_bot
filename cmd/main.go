// Package main provides the entry point for the Discord bots
package main

import (
	"errors"
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

	// Initialize the Spotify Discord bot with the spotify client
	spotifyBot := newSpotifyBot(spotifyClient, readyMessage())
	clients = append(clients, spotifyBot)

	// Wire Discord health into the debug client's /health endpoint
	debugClient.AddHealthChecker(spotifyBot)

	// Update spotify client with discord messenger
	spotifyClient.SetMessenger(spotifyBot)
	clients = append(clients, spotifyClient)

	// Start clients
	for _, client := range clients {
		if err := client.Start(); err != nil {
			logger.Fatal("Failed to start client", zap.Error(err), zap.Stringer(zapkey.Client, client))
		}
	}

	// Start the Arcade Discord bot. It's optional: if it isn't configured or
	// fails to start, the other clients keep running.
	if arcadeBot := newArcadeBot(); arcadeBot != nil {
		if err := arcadeBot.Start(); err != nil {
			logger.Error("Failed to start client", zap.Error(err), zap.Stringer(zapkey.Client, arcadeBot))
		} else {
			clients = append(clients, arcadeBot)
			debugClient.AddHealthChecker(arcadeBot)
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
	// Only the Spotify bot is required; the Arcade bot is skipped when unset
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

// newSpotifyBot creates the Discord bot that collects songs into the Spotify playlist
func newSpotifyBot(playlistAdder discord.PlaylistAdder, botReadyMessage string) *discord.Client {
	config, err := discordconfig.Load(envvar.NamespaceSpotify)
	if err != nil {
		logger.Fatal("Failed to create Spotify Discord config", zap.Error(err))
	}
	if err := config.RequireChannels(discordchannel.Auth, discordchannel.Songs); err != nil {
		logger.Fatal("Invalid Spotify Discord config", zap.Error(err))
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
	discordClient, err := discord.NewClient(discord.WithConfig(config), discord.WithHandlers(handlers...))
	if err != nil {
		logger.Fatal("Failed to create Spotify Discord client", zap.Error(err))
	}
	return discordClient
}

// newArcadeBot creates the Discord bot for the arcade.
// Returns nil if the bot isn't configured or can't be created.
func newArcadeBot() *discord.Client {
	config, err := discordconfig.Load(envvar.NamespaceArcade)
	if errors.Is(err, discordconfig.ErrNotConfigured) {
		logger.Info("Arcade Discord bot not configured; skipping", zap.Error(err))
		return nil
	}
	if err != nil {
		logger.Error("Failed to create Arcade Discord config", zap.Error(err))
		return nil
	}

	// Placeholder until the arcade commands are added: comes online, does nothing
	handlers := []discord.Handler{
		discord.NewReadyHandler("", "", ""),
	}

	discordClient, err := discord.NewClient(discord.WithConfig(config), discord.WithHandlers(handlers...))
	if err != nil {
		logger.Error("Failed to create Arcade Discord client", zap.Error(err))
		return nil
	}
	return discordClient
}
