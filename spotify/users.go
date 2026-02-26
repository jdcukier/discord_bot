package spotify

import (
	"context"

	"github.com/jdcukier/spotify/v2"
)

// -- Users ---

// currentUser gets detailed profile information about the current user.
func (c *Client) currentUser(ctx context.Context, api *spotify.Client) (*spotify.PrivateUser, error) {
	return api.CurrentUser(ctx)
}
