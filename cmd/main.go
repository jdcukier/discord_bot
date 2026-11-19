// Package main provides the entry point for the Discord bots
package main

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/debug"
	"discordbot/discord"
	discordchannel "discordbot/discord/channel"
	discordconfig "discordbot/discord/config"
	"discordbot/log"
	"discordbot/spotify"
	"discordbot/spotify/trigger"
	"discordbot/utils/httputil"
)

// defaultTagsFile is where playlist tags are saved when SPOTIFY_TAGS_FILE isn't set
const defaultTagsFile = "playlist-tags.yaml"

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
	spotifyBot, err := newSpotifyBot(spotifyClient, spotifyClient)
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

// Playlist names are looked up with the Spotify client and saved in the tags file
var (
	_ discord.PlaylistNameFetcher = (*spotify.Client)(nil)
	_ discord.PlaylistNameStore   = (*trigger.Store)(nil)
	_ discord.PlaylistNamer       = (*trigger.Store)(nil) // So /tags shows the names
)

// staticTriggers is a trigger source that never changes
type staticTriggers struct{ config *trigger.Config }

func (s staticTriggers) Config() *trigger.Config { return s.config }

// loadPlaylistTags loads the tags that route tracks to playlists other than the default.
// Tags are saved in a file so they can be changed from Discord; the first time, the file is
// created from SPOTIFY_PLAYLIST_TRIGGERS, which is ignored after that. An invalid setup
// shouldn't take the bot down, so problems are logged and the bot falls back to fewer
// features: no tags at all (nil triggers), or tags that can't be changed (nil store).
func loadPlaylistTags() (discord.TriggerSource, discord.TagStore) {
	settings, err := trigger.LoadSettings()
	if err != nil {
		logger.Error("Invalid playlist trigger settings; adding to the default playlist only", zap.Error(err))
		return nil, nil
	}

	path := os.Getenv(envvar.SpotifyTagsFile)
	if path == "" {
		path = defaultTagsFile
	}
	if storePath := trigger.StorePath(path); storePath != path {
		logger.Warn("Saving playlist tags as YAML next to the JSON file; update "+envvar.SpotifyTagsFile+" to the YAML path",
			zap.String(zapkey.Path, storePath), zap.String(zapkey.Path+"_json", path))
		path = storePath
	}
	// Tags saved by earlier versions in a JSON file are converted, so that counts as a file too
	_, statErr := os.Stat(path)
	_, legacyStatErr := os.Stat(trigger.LegacyPath(path))
	fileExists := statErr == nil || legacyStatErr == nil

	seed, seedErr := trigger.LoadSeed()
	if seedErr == nil {
		seed, seedErr = trigger.NormalizePlaylists(seed)
	}
	if seedErr != nil {
		if !fileExists {
			// Don't create an empty file: it would hide the tags once the secret is fixed
			logger.Error("Invalid playlist triggers; adding to the default playlist only", zap.Error(seedErr))
			return nil, nil
		}
		logger.Warn("Ignoring invalid "+envvar.SpotifyPlaylistTriggers+" since the playlist tags file exists", zap.Error(seedErr))
		seed = nil
	}

	store, result, err := trigger.OpenStore(path, settings, seed)
	if err != nil {
		logger.Error("Failed to open the playlist tags file; using "+envvar.SpotifyPlaylistTriggers+" and disabling /tag",
			zap.String(zapkey.Path, path), zap.Error(err))
		tags, dropped := trigger.ToTags(seed, settings.DefaultPlaylistID)
		fallback, err := trigger.New(settings.Prefix, settings.DefaultPlaylistID, tags, settings.ReplyUnknown)
		if err != nil {
			logger.Error("Invalid playlist triggers; adding to the default playlist only", zap.Error(err))
			return nil, nil
		}
		logDroppedPlaylists(fallback, dropped)
		logTriggers(fallback)
		return staticTriggers{fallback}, nil
	}
	logDroppedPlaylists(store.Config(), result.Dropped)
	logStoreMigration(path, result)
	for _, name := range result.LongNames {
		logger.Warn(fmt.Sprintf("Playlist tag is longer than %d characters; it still works, but /tag can't change or remove it",
			trigger.MaxNameLength), zap.String(zapkey.Name, settings.Prefix+name))
	}
	switch {
	case result.Created && len(seed) > 0:
		logger.Info("Created the playlist tags file from "+envvar.SpotifyPlaylistTriggers, zap.String(zapkey.Path, path))
	case result.Created:
		logger.Info("Created an empty playlist tags file", zap.String(zapkey.Path, path))
	case os.Getenv(envvar.SpotifyPlaylistTriggers) != "":
		logger.Info("Using the playlist tags file; "+envvar.SpotifyPlaylistTriggers+" is only used to create it and can be removed",
			zap.String(zapkey.Path, path))
	}
	logTriggers(store.Config())
	return store, store
}

// logStoreMigration logs what OpenStore did with the JSON tags file of earlier versions, if anything,
// and problems it worked around
func logStoreMigration(path string, result trigger.OpenResult) {
	switch {
	case result.YAMLBackup != "":
		logger.Warn("The JSON playlist tags file is newer than the YAML file, e.g. after a rollback; "+
			"converted it again and backed up the YAML file",
			zap.String(zapkey.Path, path),
			zap.String(zapkey.Path+"_yaml_backup", result.YAMLBackup),
			zap.String(zapkey.Path+"_json_backup", result.Backup))
	case result.Converted && result.Backup != "":
		logger.Info("Converted the playlist tags file to YAML and backed up the JSON file",
			zap.String(zapkey.Path, path), zap.String(zapkey.Path+"_json_backup", result.Backup))
	case result.Backup != "":
		logger.Info("Backed up the JSON playlist tags file left over from an interrupted conversion; using the YAML file",
			zap.String(zapkey.Path, path), zap.String(zapkey.Path+"_json_backup", result.Backup))
	}
	if result.SaveErr != nil {
		logger.Warn("Failed to save the converted playlist tags file; using the converted tags anyway and retrying on the next change",
			zap.String(zapkey.Path, path), zap.Error(result.SaveErr))
	}
	if result.BackupErr != nil {
		logger.Warn("Problem backing up a playlist tags file; using the YAML file anyway",
			zap.String(zapkey.Path, path), zap.Error(result.BackupErr))
	}
	if result.DefaultErr != nil {
		logger.Warn("Ignoring the default playlist's name in the playlist tags file; it's rewritten on the next save",
			zap.String(zapkey.Path, path), zap.Error(result.DefaultErr))
	}
}

// logDroppedPlaylists warns about tags that lost playlists when tags became one playlist each.
// IDs are shortened like everywhere else in the logs.
func logDroppedPlaylists(c *trigger.Config, dropped map[string][]string) {
	for _, name := range slices.Sorted(maps.Keys(dropped)) {
		var ids []string
		for _, id := range dropped[name] {
			if id == c.DefaultPlaylistID {
				ids = append(ids, "default playlist")
			} else {
				ids = append(ids, log.RedactID(id))
			}
		}
		logger.Warn("Tag now points to one playlist; use "+trigger.PlusPrefix+name+" to also add to the default playlist, "+
			"or /tag add a new tag for each dropped playlist",
			zap.String(zapkey.Name, c.Prefix+name),
			zap.String(zapkey.PlaylistID, log.RedactID(c.Tags[name])),
			zap.Strings(zapkey.PlaylistIDs+"_dropped", ids))
	}
}

// logTriggers logs the names of the loaded triggers
func logTriggers(c *trigger.Config) {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(c.Tags)) {
		names = append(names, c.Prefix+name)
	}
	logger.Info("Loaded playlist triggers", zap.Strings(zapkey.Triggers, names))
}

// newSpotifyBot creates the Discord bot that collects songs into the Spotify playlist.
// nameFetcher looks up the names of tagged playlists, which are saved with the tags.
func newSpotifyBot(playlistAdder discord.PlaylistAdder, nameFetcher discord.PlaylistNameFetcher) (*discord.Client, error) {
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
	triggers, tags := loadPlaylistTags()
	var names *discord.PlaylistNames
	if nameStore, ok := tags.(discord.PlaylistNameStore); ok {
		names = discord.NewPlaylistNames(nameFetcher, nameStore)
	}
	handlers := []discord.Handler{
		discord.NewReadyHandler(songsChannelID, withVersion(readyMessage), listeningMessage),
		discord.NewMessageHandler(playlistAdder, actions,
			discord.WithPlaylistTriggers(triggers), discord.WithPlaylistNames(names)),
		discord.NewInteractionSessionHandler(
			discord.WithTagCommands(config.AppID, tags), discord.WithTagPlaylistNames(names)),
	}

	// Create the client
	return discord.NewClient(discord.WithConfig(config), discord.WithHandlers(handlers...))
}
