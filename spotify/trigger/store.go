package trigger

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// storeFile is the on-disk format of the store
type storeFile struct {
	Tags map[string][]string `json:"tags"`
}

// Store holds the trigger configuration and persists tag changes to a JSON file.
// Reads are lock-free; writes are serialized and replace the whole configuration.
type Store struct {
	path   string
	mu     sync.Mutex // Serializes writes
	config atomic.Pointer[Config]
}

// OpenStore loads the tags saved at path and applies them to base.
// If no file exists yet, base's current tags are saved there, so a one-time
// import from SPOTIFY_PLAYLIST_TRIGGERS carries over. imported reports whether that happened.
func OpenStore(path string, base *Config) (store *Store, imported bool, err error) {
	if base == nil {
		return nil, false, fmt.Errorf("base config is nil")
	}
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := s.save(base.Playlists); err != nil {
			return nil, false, err
		}
		s.config.Store(base)
		return s, true, nil
	case err != nil:
		return nil, false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, false, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	for name := range file.Tags {
		if err := ValidateName(name); err != nil {
			return nil, false, fmt.Errorf("invalid tag in %s: %w", path, err)
		}
	}
	config, err := base.withPlaylists(file.Tags)
	if err != nil {
		return nil, false, fmt.Errorf("invalid tags in %s: %w", path, err)
	}
	s.config.Store(config)
	return s, false, nil
}

// Config returns the current trigger configuration. It must not be modified.
func (s *Store) Config() *Config {
	return s.config.Load()
}

// Add adds a playlist to a tag, creating the tag if needed, and returns the tag's playlists.
// added is false if the tag already pointed to the playlist.
func (s *Store) Add(name, playlistID string) (playlistIDs []string, added bool, err error) {
	if err := ValidateName(name); err != nil {
		return nil, false, err
	}
	if !playlistIDRegex.MatchString(playlistID) {
		return nil, false, fmt.Errorf("invalid playlist ID %q", playlistID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Config()
	if slices.Contains(current.Playlists[name], playlistID) {
		return current.Playlists[name], false, nil
	}
	playlists := clonePlaylists(current.Playlists)
	playlists[name] = append(playlists[name], playlistID)
	if err := s.update(current, playlists); err != nil {
		return nil, false, err
	}
	return playlists[name], true, nil
}

// Remove removes a playlist from a tag, or the whole tag if playlistID is empty.
// It returns the playlists the tag still points to, which is empty if the tag was deleted.
func (s *Store) Remove(name, playlistID string) (remaining []string, err error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Config()
	ids, ok := current.Playlists[name]
	if !ok {
		return nil, fmt.Errorf("there's no %s%s tag", current.Prefix, name)
	}
	playlists := clonePlaylists(current.Playlists)
	if playlistID == "" {
		delete(playlists, name)
	} else {
		if !slices.Contains(ids, playlistID) {
			return nil, fmt.Errorf("%s%s doesn't add to that playlist", current.Prefix, name)
		}
		remaining = slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == playlistID })
		if len(remaining) == 0 {
			delete(playlists, name)
		} else {
			playlists[name] = remaining
		}
	}
	if err := s.update(current, playlists); err != nil {
		return nil, err
	}
	return remaining, nil
}

// update saves the new tags and makes them current. The caller must hold s.mu.
func (s *Store) update(current *Config, playlists map[string][]string) error {
	config, err := current.withPlaylists(playlists)
	if err != nil {
		return err
	}
	if err := s.save(playlists); err != nil {
		return err
	}
	s.config.Store(config)
	return nil
}

// save writes the tags to disk atomically, so a crash never leaves a half-written file
func (s *Store) save(playlists map[string][]string) error {
	if playlists == nil {
		playlists = map[string][]string{}
	}
	data, err := json.MarshalIndent(storeFile{Tags: playlists}, "", "  ")
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}
	return nil
}

// withPlaylists returns a copy of c with different tags
func (c *Config) withPlaylists(playlists map[string][]string) (*Config, error) {
	return New(c.Prefix, c.DefaultPlaylistID, playlists, c.ReplyUnknown)
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

// clonePlaylists deep-copies a tag mapping
func clonePlaylists(playlists map[string][]string) map[string][]string {
	out := make(map[string][]string, len(playlists))
	for name, ids := range maps.All(playlists) {
		out[name] = slices.Clone(ids)
	}
	return out
}
