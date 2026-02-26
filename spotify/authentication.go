package spotify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"discordbot/constants/zapkey"
	"discordbot/discord/channel"
	"discordbot/spotify/worker"
)

const (
	authPollInterval = 10 * time.Second
	authPollTimeout  = 3 * time.Minute
)

func (c *Client) authenticate(ctx context.Context, userID string) error {
	authURL, err := c.workerClient.GetAuthURL(ctx, userID)
	if err != nil {
		return fmt.Errorf("fetching auth URL from worker: %w", err)
	}

	message := fmt.Sprintf(
		"🎵 <@%s> Spotify authentication required. Open this URL to connect:\n%s", userID, authURL)
	if c.messenger != nil {
		if err := c.messenger.SendMessage(ctx, channel.Auth.String(), message); err != nil {
			logger.Error("Failed to post auth URL to Discord; user must re-trigger auth",
				zap.Error(err), zap.String(zapkey.UserID, userID))
		}
	} else {
		logger.Warn("Messenger not configured; auth URL cannot be delivered to user",
			zap.String(zapkey.UserID, userID))
	}

	return c.pollForToken(ctx, userID)
}

func (c *Client) pollForToken(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, authPollTimeout)
	defer cancel()

	if token, err := c.workerClient.GetToken(ctx, userID); err == nil && token != nil {
		logger.Info("OAuth token received immediately", zap.String(zapkey.UserID, userID))
		return nil
	}

	ticker := time.NewTicker(authPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			token, err := c.workerClient.GetToken(ctx, userID)
			if errors.Is(err, worker.ErrAuthRequired) {
				logger.Debug("Waiting for user to complete OAuth",
					zap.String(zapkey.UserID, userID))
				continue
			}
			if err != nil {
				logger.Warn("Transient error polling for token; retrying",
					zap.Error(err), zap.String(zapkey.UserID, userID))
				continue
			}
			if token != nil {
				logger.Info("OAuth token received; authentication complete",
					zap.String(zapkey.UserID, userID))
				return nil
			}

		case <-ctx.Done():
			return fmt.Errorf("authentication timed out after %s", authPollTimeout)
		}
	}
}
