package spotify

import (
	"context"
	"fmt"

	"github.com/jdcukier/spotify/v2"
)

// -- Users ---

// CurrentUser gets detailed profile information about the current user.
func (c *Client) CurrentUser(ctx context.Context) (*spotify.PrivateUser, error) {
	if c.api == nil {
		return nil, fmt.Errorf("spotify client not initialized")
	}
	return c.api.CurrentUser(ctx)
}
