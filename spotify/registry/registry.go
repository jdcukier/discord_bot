package registry

import (
	"encoding/json"
	"strings"
)

// PlaylistEntry maps a Spotify playlist to a set of genre keywords.
type PlaylistEntry struct {
	Name   string   `json:"name"`
	ID     string   `json:"id"`
	Genres []string `json:"genres"`
}

// Registry holds a collection of genre-to-playlist mappings.
type Registry struct {
	entries []PlaylistEntry
}

// Load parses a JSON array of playlist entries from the given string.
func Load(configJSON string) (*Registry, error) {
	var entries []PlaylistEntry
	if err := json.Unmarshal([]byte(configJSON), &entries); err != nil {
		return nil, err
	}
	return &Registry{entries: entries}, nil
}

// Match returns all playlist entries whose genre keywords match any of the provided genres.
// A keyword matches if it is a case-insensitive substring of any genre string.
func (r *Registry) Match(genres []string) []PlaylistEntry {
	if r == nil {
		return nil
	}
	var matched []PlaylistEntry
	for _, entry := range r.entries {
		if matchesAny(entry.Genres, genres) {
			matched = append(matched, entry)
		}
	}
	return matched
}

// matchesAny returns true if any keyword from keywords is a case-insensitive substring of any genre.
func matchesAny(keywords, genres []string) bool {
	for _, kw := range keywords {
		kwLower := strings.ToLower(kw)
		for _, g := range genres {
			if strings.Contains(strings.ToLower(g), kwLower) {
				return true
			}
		}
	}
	return false
}
