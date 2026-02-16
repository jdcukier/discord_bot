// Package main provides the entry point for the Discord bot
package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"discordbot/constants/id"
	"discordbot/constants/zapkey"
	"discordbot/debug"
	"discordbot/discord"
	"discordbot/spotify"
	"discordbot/utils/httputil"
)

type Clients interface {
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
	var clients []Clients

	// Initialize Debug client
	// Note: This must be initialized first since it hosts the root HTTP path
	debugClient, err := debug.NewClient()
	if err != nil {
		logger.Fatal("Failed to create Debug client", zap.Error(err))
	}
	clients = append(clients, debugClient)

	// Initialize Spotify client (needs server to be running)
	spotifyClient, err := spotify.NewClient()
	if err != nil {
		logger.Fatal("Failed to create Spotify client", zap.Error(err))
	}
	clients = append(clients, spotifyClient)

	// Initialize Discord client
	discordClient := newDiscordClient(spotifyClient)
	clients = append(clients, discordClient)

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
	// Actions to perform when a message is received
	actions := make(discord.ChannelActions)
	actions.Add(id.ChannelIDTest, discord.ActionReply)
	actions.Add(id.ChannelIDTest, discord.ActionAddTracksToPlaylist)
	actions.Add(id.ChannelIDBangers, discord.ActionAddTracksToPlaylist)

	// Handlers
	handlers := []discord.Handler{
		discord.NewMessageHandler(playlistAdder, actions),
		discord.NewInteractionSessionHandler(),
	}

	// Create the client
	discordClient, err := discord.NewClient(handlers...)
	if err != nil {
		logger.Fatal("Failed to create Discord client", zap.Error(err))
	}
	return discordClient
}
