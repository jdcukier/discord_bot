// Package main provides the entry point for the Discord bot
package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

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

	// Load .env file
	if err := godotenv.Load(); err != nil {
		logger.Fatal("Failed to load .env file", zap.Error(err))
	}

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

	// Initialize a pointer for the spotify client. We'll assign it later.
	// TODO: There's probably a better way to do this.
	var spotifyClient *spotify.Client

	// Initialize Discord client
	discordClient := newDiscordClient(spotifyClient)
	clients = append(clients, discordClient)

	// Initialize Spotify client (needs server to be running)
	spotifyClient, err = spotify.NewClient(spotify.WithMessenger(discordClient))
	if err != nil {
		logger.Fatal("Failed to create Spotify client", zap.Error(err))
	}
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

func newDiscordClient(playlistAdder discord.PlaylistAdder) *discord.Client {
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

	// Handlers
	handlers := []discord.Handler{
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
