package track

import (
	"regexp"
	"strings"

	"github.com/jdcukier/spotify/v2"
)

// extractURLs extracts all URLs from the given content using regex
func extractURLs(content string) []string {
	// Regex pattern to match URLs
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)
	return urlRegex.FindAllString(content, -1)
}

// ExtractURLs extracts all Spotify track URLs from the given content
func ExtractURLs(content string) ([]string, bool) {
	urls := extractURLs(content)
	var spotifyTracks []string

	for _, url := range urls {
		if strings.Contains(url, "open.spotify.com/track") {
			spotifyTracks = append(spotifyTracks, url)
		}
	}

	return spotifyTracks, len(spotifyTracks) > 0
}

// ExtractTrackID extracts the track ID from a Spotify track URL
func ExtractTrackID(url string) string {
	// Regex to match Spotify track URLs and capture everything after '/track/' until the query indicator '?' or end
	re := regexp.MustCompile(`open\.spotify\.com/track/([^?]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// ToTrackIDs converts Spotify track URLs to spotify.ID slice
func ToTrackIDs(urls []string) []spotify.ID {
	var trackIDs []spotify.ID
	for _, trackURL := range urls {
		trackID := ExtractTrackID(trackURL)
		if trackID != "" {
			trackIDs = append(trackIDs, spotify.ID(trackID))
		}
	}
	return trackIDs
}

// FilterTracks returns a list of tracks that are not already in the playlist
func FilterTracks(playlist *spotify.PlaylistItemPage, trackIDs []spotify.ID) []spotify.ID {
	if playlist == nil || len(playlist.Items) == 0 {
		return trackIDs
	}

	existingTracks := make(map[spotify.ID]struct{})
	for _, playlistItem := range playlist.Items {
		// This should never happen, but just in case
		if playlistItem.Item.Track == nil {
			continue
		}
		// Add track to existing tracks
		existingTracks[playlistItem.Item.Track.ID] = struct{}{}
	}

	// Filter out tracks that are already in the playlist
	var filteredTracks []spotify.ID
	for _, trackID := range trackIDs {
		if _, ok := existingTracks[trackID]; !ok {
			filteredTracks = append(filteredTracks, trackID)
		}
	}

	return filteredTracks
}
