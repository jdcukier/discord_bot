package trigger

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// editorialPrefix starts the IDs of Spotify-owned editorial playlists, which nobody else can add to
const editorialPrefix = "37i9dQZF1"

// playlistIDRegex matches a Spotify playlist ID: 22 base-62 characters
var playlistIDRegex = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

// playlistLinkRegex extracts the ID from a playlist link or URI, e.g.
// https://open.spotify.com/playlist/<id>?si=..., open.spotify.com/intl-fr/playlist/<id>, or spotify:playlist:<id>
var playlistLinkRegex = regexp.MustCompile(`(?:/playlist/|^spotify:playlist:)([0-9A-Za-z]+)`)

// storeFile is the on-disk format of the store: tag name to playlist ID
type storeFile struct {
	Tags map[string]string `json:"tags"`
}

// legacyStoreFile is the earlier format, with a list of playlists per tag
type legacyStoreFile struct {
	Tags map[string][]string `json:"tags"`
}

// Store holds the trigger configuration and persists tag changes to a JSON file.
// Reads are lock-free; writes are serialized and replace the whole configuration.
type Store struct {
	path   string
	mu     sync.Mutex // Serializes writes
	config atomic.Pointer[Config]
}

// OpenResult describes what OpenStore did besides loading the tags
type OpenResult struct {
	Created bool     // The file didn't exist and was created from the seed
	Changed []string // Tags converted from several playlists to one
}

// OpenStore loads the tags saved at path, using settings for everything else
// (settings' own tags are ignored). If no file exists yet, it's created from seed,
// the tags in SPOTIFY_PLAYLIST_TRIGGERS. Files in the earlier format, with several
// playlists per tag, are converted with ToTags and saved in the current format.
func OpenStore(path string, settings *Config, seed map[string][]string) (*Store, OpenResult, error) {
	var result OpenResult
	if settings == nil {
		return nil, result, fmt.Errorf("settings are nil")
	}
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		seed, err := NormalizePlaylists(seed)
		if err != nil {
			return nil, result, fmt.Errorf("invalid tags to import: %w", err)
		}
		var tags map[string]string
		tags, result.Changed = ToTags(seed, settings.DefaultPlaylistID)
		result.Created = true
		return s, result, s.init(settings, tags, true)
	case err != nil:
		return nil, result, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		// Try the earlier format
		var legacy legacyStoreFile
		if legacyErr := json.Unmarshal(data, &legacy); legacyErr != nil {
			return nil, result, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		playlists, err := NormalizePlaylists(legacy.Tags)
		if err != nil {
			return nil, result, fmt.Errorf("invalid tags in %s: %w", path, err)
		}
		var tags map[string]string
		tags, result.Changed = ToTags(playlists, settings.DefaultPlaylistID)
		if err := s.init(settings, tags, true); err != nil {
			return nil, result, fmt.Errorf("failed to convert %s: %w", path, err)
		}
		return s, result, nil
	}
	tags, err := normalizeTags(file.Tags)
	if err != nil {
		return nil, result, fmt.Errorf("invalid tags in %s: %w", path, err)
	}
	if err := s.init(settings, tags, false); err != nil {
		return nil, result, fmt.Errorf("invalid tags in %s: %w", path, err)
	}
	return s, result, nil
}

// init sets the store's initial tags, saving them if needed
func (s *Store) init(settings *Config, tags map[string]string, save bool) error {
	config, err := settings.withTags(tags)
	if err != nil {
		return err
	}
	if save {
		if err := s.save(tags); err != nil {
			return err
		}
	}
	s.config.Store(config)
	return nil
}

// normalizeTags validates tag names and converts playlist links to bare IDs,
// so stored IDs always match what the commands extract
func normalizeTags(tags map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(tags))
	for name, raw := range tags {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		id, err := ExtractPlaylistID(raw)
		if err != nil {
			return nil, fmt.Errorf("tag %q: %w", name, err)
		}
		out[name] = id
	}
	return out, nil
}

// NormalizePlaylists is normalizeTags for several playlists per tag, as in SPOTIFY_PLAYLIST_TRIGGERS.
// Tags without playlists are dropped.
func NormalizePlaylists(playlists map[string][]string) (map[string][]string, error) {
	out := make(map[string][]string, len(playlists))
	for name, ids := range playlists {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		for _, raw := range ids {
			id, err := ExtractPlaylistID(raw)
			if err != nil {
				return nil, fmt.Errorf("tag %q: %w", name, err)
			}
			out[name] = appendUnique(out[name], id)
		}
	}
	return out, nil
}

// Config returns the current trigger configuration. It must not be modified.
func (s *Store) Config() *Config {
	return s.config.Load()
}

// Add creates a tag pointing to a playlist. If the tag already exists, it's left
// unchanged and its playlist is returned as existing.
func (s *Store) Add(name, playlistID string) (existing string, err error) {
	return s.set(name, playlistID, false)
}

// Set points a tag to a playlist, creating the tag if needed.
// It returns the playlist the tag pointed to before, if any.
func (s *Store) Set(name, playlistID string) (previous string, err error) {
	return s.set(name, playlistID, true)
}

// set creates a tag, or replaces its playlist if replace is true, and returns its previous playlist
func (s *Store) set(name, playlistID string, replace bool) (previous string, err error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	if !playlistIDRegex.MatchString(playlistID) {
		return "", fmt.Errorf("invalid playlist ID %q", playlistID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Config()
	previous = current.Tags[name]
	if previous == playlistID || (previous != "" && !replace) {
		return previous, nil
	}
	tags := maps.Clone(current.Tags)
	tags[name] = playlistID
	return previous, s.update(current, tags)
}

// Remove deletes a tag and returns the playlist it pointed to
func (s *Store) Remove(name string) (previous string, err error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Config()
	previous, ok := current.Tags[name]
	if !ok {
		return "", fmt.Errorf("there's no %s%s tag", current.Prefix, name)
	}
	tags := maps.Clone(current.Tags)
	delete(tags, name)
	return previous, s.update(current, tags)
}

// update saves the new tags and makes them current. The caller must hold s.mu.
func (s *Store) update(current *Config, tags map[string]string) error {
	config, err := current.withTags(tags)
	if err != nil {
		return err
	}
	if err := s.save(tags); err != nil {
		return err
	}
	s.config.Store(config)
	return nil
}

// save writes the tags to disk atomically, so a crash never leaves a half-written file
func (s *Store) save(tags map[string]string) error {
	if tags == nil {
		tags = map[string]string{}
	}
	data, err := json.MarshalIndent(storeFile{Tags: tags}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode tags: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}
	defer os.Remove(tmp.Name()) // No-op after a successful rename
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to save tags: %w", err)
	}
	// Flush the data before the rename, so the rename can't reach the disk first
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to save tags: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}
	// Flush the rename itself
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}
	return nil
}

// syncDir flushes a directory's entries to disk
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// withTags returns a copy of c with different tags
func (c *Config) withTags(tags map[string]string) (*Config, error) {
	return New(c.Prefix, c.DefaultPlaylistID, tags, c.ReplyUnknown)
}

// ValidateName checks that a tag name (without the prefix) is lowercase, valid, and not reserved
func ValidateName(name string) error {
	if !nameRegex.MatchString(name) || name != strings.ToLower(name) {
		return fmt.Errorf("invalid tag name %q: use letters and numbers, optionally joined by a single - or _", name)
	}
	if name == DefaultName {
		return fmt.Errorf("%q is reserved for the default playlist", DefaultName)
	}
	return nil
}

// ParsePlaylistID extracts the playlist ID from a Spotify playlist link, URI, or bare ID.
// Spotify-owned editorial playlists are rejected, since nobody else can add to them.
func ParsePlaylistID(input string) (string, error) {
	id, err := ExtractPlaylistID(input)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(id, editorialPrefix) {
		return "", fmt.Errorf("that's a Spotify-made playlist, which nobody can add songs to; copy its songs into a collaborative playlist instead")
	}
	return id, nil
}

// ExtractPlaylistID extracts the playlist ID from a Spotify playlist link, URI, or bare ID
func ExtractPlaylistID(input string) (string, error) {
	input = strings.TrimSpace(input)
	id := input
	if m := playlistLinkRegex.FindStringSubmatch(input); m != nil {
		id = m[1]
	} else if strings.ContainsAny(input, "/:") {
		return "", fmt.Errorf("that isn't a Spotify playlist link")
	}
	if !playlistIDRegex.MatchString(id) {
		return "", fmt.Errorf("%q isn't a valid Spotify playlist ID", id)
	}
	return id, nil
}
