// Package trigger maps keywords in a message (e.g. "!relaxing") to Spotify playlists
package trigger

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"discordbot/constants/envvar"
)

const (
	// DefaultPrefix marks a word as a trigger when no prefix is configured
	DefaultPrefix = "!"

	// DefaultName is the reserved trigger for the default playlist
	DefaultName = "default"
)

// namePattern matches a valid trigger name (without the prefix).
// Names may contain single '-' or '_' separators but can't start or end with them, so "!gym-" and "__!gym__" match "gym".
const namePattern = `[\p{L}\p{N}]+(?:[_-][\p{L}\p{N}]+)*`

// boundaryPattern matches what may come right before the prefix: the start of the message,
// any space, opening brackets/quotes, or Discord markdown (bold, italics, strikethrough, spoilers, code, quotes)
const boundaryPattern = `(?:^|[\s\p{Z}\p{Ps}\p{Pi}*_~|"'\x60>])`

var nameRegex = regexp.MustCompile(`^` + namePattern + `$`)

// Config represents the trigger to playlist mapping
type Config struct {
	Prefix            string              // Single character that marks a word as a trigger
	DefaultPlaylistID string              // Playlist used when no known trigger is present
	Playlists         map[string][]string // Lowercase trigger name to playlist IDs
	ReplyUnknown      bool                // Reply to messages that use an unknown trigger

	triggerRegex *regexp.Regexp
}

// Load creates the trigger configuration from env vars
func Load() (*Config, error) {
	prefix := os.Getenv(envvar.SpotifyTriggerPrefix)
	if prefix == "" {
		prefix = DefaultPrefix
	}

	playlists, err := ParsePlaylists(os.Getenv(envvar.SpotifyPlaylistTriggers))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", envvar.SpotifyPlaylistTriggers, err)
	}

	var replyUnknown bool
	if v := os.Getenv(envvar.SpotifyReplyUnknownTriggers); v != "" {
		if replyUnknown, err = strconv.ParseBool(v); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", envvar.SpotifyReplyUnknownTriggers, err)
		}
	}

	return New(prefix, os.Getenv(envvar.SpotifyPlaylistID), playlists, replyUnknown)
}

// New creates and validates a trigger configuration
func New(prefix, defaultPlaylistID string, playlists map[string][]string, replyUnknown bool) (*Config, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	if defaultPlaylistID == "" {
		return nil, fmt.Errorf("default playlist ID is empty")
	}
	if _, ok := playlists[DefaultName]; ok {
		return nil, fmt.Errorf("%q is reserved for the default playlist", DefaultName)
	}
	return &Config{
		Prefix:            prefix,
		DefaultPlaylistID: defaultPlaylistID,
		Playlists:         playlists,
		ReplyUnknown:      replyUnknown,
		triggerRegex:      regexp.MustCompile(boundaryPattern + regexp.QuoteMeta(prefix) + `(` + namePattern + `)`),
	}, nil
}

// ParsePlaylists parses a mapping in the form "relaxing=id1,id2;chill=id1".
// Names are case-insensitive, and repeated names are merged.
func ParsePlaylists(s string) (map[string][]string, error) {
	playlists := make(map[string][]string)
	for entry := range strings.SplitSeq(s, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, ids, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("entry %q is missing '='", entry)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if !nameRegex.MatchString(name) {
			return nil, fmt.Errorf("invalid trigger name %q", name)
		}
		for id := range strings.SplitSeq(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				playlists[name] = appendUnique(playlists[name], id)
			}
		}
		if len(playlists[name]) == 0 {
			return nil, fmt.Errorf("trigger %q has no playlist IDs", name)
		}
	}
	return playlists, nil
}

// Extract returns the lowercase trigger names in the content, in order and without duplicates
func (c *Config) Extract(content string) []string {
	var names []string
	for _, match := range c.triggerRegex.FindAllStringSubmatch(content, -1) {
		names = appendUnique(names, strings.ToLower(match[1]))
	}
	return names
}

// Resolve returns the playlists the content's triggers map to, without duplicates,
// along with any unknown trigger names. If no known trigger is present, the
// default playlist is returned.
func (c *Config) Resolve(content string) (playlistIDs []string, unknown []string) {
	for _, name := range c.Extract(content) {
		if name == DefaultName {
			playlistIDs = appendUnique(playlistIDs, c.DefaultPlaylistID)
			continue
		}
		ids, ok := c.Playlists[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		for _, id := range ids {
			playlistIDs = appendUnique(playlistIDs, id)
		}
	}
	if len(playlistIDs) == 0 {
		playlistIDs = []string{c.DefaultPlaylistID}
	}
	return playlistIDs, unknown
}

// validatePrefix checks that the prefix is a single non-alphanumeric, non-space character
func validatePrefix(prefix string) error {
	r, size := utf8.DecodeRuneInString(prefix)
	if size == 0 || size != len(prefix) {
		return fmt.Errorf("trigger prefix %q must be a single character", prefix)
	}
	if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) || r == '_' || r == '-' {
		return fmt.Errorf("trigger prefix %q must be a symbol", prefix)
	}
	return nil
}

// appendUnique appends s to list if it isn't already present
func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
