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

	// PlusPrefix marks a trigger that also adds to the default playlist, e.g. "+relax"
	PlusPrefix = "+"

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
	Prefix            string            // Single character that marks a word as a trigger
	DefaultPlaylistID string            // Playlist used when no known trigger is present
	Tags              map[string]string // Lowercase trigger name to its playlist ID
	ReplyUnknown      bool              // Reply to messages that use an unknown trigger

	triggerRegex *regexp.Regexp
}

// Trigger is a trigger found in a message
type Trigger struct {
	Name string // Lowercase name, without the prefix
	Plus bool   // Used with PlusPrefix, so the default playlist is added too
}

// Load creates the trigger configuration from env vars
func Load() (*Config, error) {
	settings, err := LoadSettings()
	if err != nil {
		return nil, err
	}
	playlists, err := LoadSeed()
	if err != nil {
		return nil, err
	}
	tags, _ := ToTags(playlists, settings.DefaultPlaylistID)
	return settings.withTags(tags)
}

// LoadSettings creates the trigger configuration from env vars, without any tags
func LoadSettings() (*Config, error) {
	prefix := os.Getenv(envvar.SpotifyTriggerPrefix)
	if prefix == "" {
		prefix = DefaultPrefix
	}

	var replyUnknown bool
	if v := os.Getenv(envvar.SpotifyReplyUnknownTriggers); v != "" {
		var err error
		if replyUnknown, err = strconv.ParseBool(v); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", envvar.SpotifyReplyUnknownTriggers, err)
		}
	}

	return New(prefix, os.Getenv(envvar.SpotifyPlaylistID), nil, replyUnknown)
}

// LoadSeed parses the tags in SPOTIFY_PLAYLIST_TRIGGERS, which seed the tags file.
// The secret may list several playlists per tag; ToTags converts them to one each.
func LoadSeed() (map[string][]string, error) {
	playlists, err := ParsePlaylists(os.Getenv(envvar.SpotifyPlaylistTriggers))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", envvar.SpotifyPlaylistTriggers, err)
	}
	return playlists, nil
}

// New creates and validates a trigger configuration
func New(prefix, defaultPlaylistID string, tags map[string]string, replyUnknown bool) (*Config, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	if defaultPlaylistID == "" {
		return nil, fmt.Errorf("default playlist ID is empty")
	}
	if _, ok := tags[DefaultName]; ok {
		return nil, fmt.Errorf("%q is reserved for the default playlist", DefaultName)
	}
	if tags == nil {
		tags = map[string]string{}
	}
	prefixes := regexp.QuoteMeta(prefix) + regexp.QuoteMeta(PlusPrefix)
	return &Config{
		Prefix:            prefix,
		DefaultPlaylistID: defaultPlaylistID,
		Tags:              tags,
		ReplyUnknown:      replyUnknown,
		triggerRegex:      regexp.MustCompile(boundaryPattern + `([` + prefixes + `])(` + namePattern + `)`),
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

// ToTags converts tags with several playlists to one playlist each, since "+tag" now
// covers the default playlist. A tag keeps its first playlist other than the default one,
// or the default playlist if that's all it had. dropped maps each tag that lost playlists to them.
func ToTags(playlists map[string][]string, defaultPlaylistID string) (tags map[string]string, dropped map[string][]string) {
	tags = make(map[string]string, len(playlists))
	dropped = make(map[string][]string)
	for name, ids := range playlists {
		if len(ids) == 0 {
			continue
		}
		others := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == defaultPlaylistID })
		kept := defaultPlaylistID
		if len(others) > 0 {
			kept = others[0]
		}
		tags[name] = kept
		for _, id := range ids {
			if id != kept {
				dropped[name] = append(dropped[name], id)
			}
		}
	}
	return tags, dropped
}

// Extract returns the triggers in the content, in order and without duplicates
func (c *Config) Extract(content string) []Trigger {
	var triggers []Trigger
	for _, match := range c.triggerRegex.FindAllStringSubmatch(content, -1) {
		t := Trigger{Name: strings.ToLower(match[2]), Plus: match[1] == PlusPrefix}
		if !slices.Contains(triggers, t) {
			triggers = append(triggers, t)
		}
	}
	return triggers
}

// Resolve returns the playlists the content's triggers map to, without duplicates,
// along with any unknown triggers as written (e.g. "!typo"). If no known trigger
// is present, the default playlist is returned.
func (c *Config) Resolve(content string) (playlistIDs []string, unknown []string) {
	for _, t := range c.Extract(content) {
		if t.Name == DefaultName {
			playlistIDs = appendUnique(playlistIDs, c.DefaultPlaylistID)
			continue
		}
		id, ok := c.Tags[t.Name]
		if !ok {
			// Unknown "+" words are usually ordinary text ("+1", "(+bonus track)"), not typos
			if !t.Plus {
				unknown = append(unknown, c.Format(t))
			}
			continue
		}
		playlistIDs = appendUnique(playlistIDs, id)
		if t.Plus {
			playlistIDs = appendUnique(playlistIDs, c.DefaultPlaylistID)
		}
	}
	if len(playlistIDs) == 0 {
		playlistIDs = []string{c.DefaultPlaylistID}
	}
	return playlistIDs, unknown
}

// Format writes a trigger as it's typed, e.g. "!relax" or "+relax"
func (c *Config) Format(t Trigger) string {
	if t.Plus {
		return PlusPrefix + t.Name
	}
	return c.Prefix + t.Name
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
	if prefix == PlusPrefix {
		return fmt.Errorf("trigger prefix can't be %q: it marks tags that also add to the default playlist", PlusPrefix)
	}
	if prefix == "`" {
		return fmt.Errorf("trigger prefix can't be %q: it marks code in Discord messages", prefix)
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
