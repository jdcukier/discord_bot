package spotify

import (
	"context"

	"github.com/jdcukier/spotify/v2"
)

// primaryArtistGenres fetches the track name, primary artist name, and their genres for a given track ID.
// Returns empty slices (not an error) when the track has no artists or the artist has no genre data.
func (c *Client) primaryArtistGenres(ctx context.Context, api *spotify.Client, trackID spotify.ID) (trackName string, artistName string, genres []string, err error) {
	track, err := api.GetTrack(ctx, trackID)
	if err != nil {
		return "", "", nil, err
	}
	trackName = track.Name

	if len(track.Artists) == 0 {
		return trackName, "", nil, nil
	}

	primaryArtist := track.Artists[0]
	fullArtist, err := api.GetArtist(ctx, primaryArtist.ID)
	if err != nil {
		return trackName, primaryArtist.Name, nil, err
	}

	return trackName, fullArtist.Name, fullArtist.Genres, nil
}
