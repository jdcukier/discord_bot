package spotify

import (
	"regexp"
	"strings"
)

// extractURLs extracts all URLs from the given content using regex
func extractURLs(content string) []string {
	// Regex pattern to match URLs
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)
	return urlRegex.FindAllString(content, -1)
}

// ExtractTracks extracts all Spotify track URLs from the given content
func ExtractTracks(content string) ([]string, bool) {
	urls := extractURLs(content)
	var spotifyTracks []string

	for _, url := range urls {
		if strings.Contains(url, "open.spotify.com/track") {
			spotifyTracks = append(spotifyTracks, url)
		}
	}

	return spotifyTracks, len(spotifyTracks) > 0
}
