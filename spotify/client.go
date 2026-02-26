package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/jdcukier/spotify/v2"
	"go.uber.org/zap"

	"discordbot/constants/zapkey"
	"discordbot/discord/channel"
	"discordbot/spotify/config"
	"discordbot/spotify/worker"
)

// MessageSender is an interface for posting messages
// This will primarily be used for posting the Spotify Auth link to the user instead of
// needing to check the logs to find it.
// Note: This may be expanded to support other message posting in the future.
type MessageSender interface {
	SendMessage(ctx context.Context, channelType string, message string) error
}

// pendingEntry holds queued post-auth callbacks for one user.
type pendingEntry struct {
	callbacks []func(context.Context)
}

// Client represents a spotify client
type Client struct {
	// Clients
	messenger MessageSender

	// Configuration
	config *config.Config

	// Cloudflare Worker client
	workerClient *worker.Client

	// Per-user auth tracking. authMu protects authenticatingUsers and each entry's callbacks.
	authMu              sync.Mutex
	authenticatingUsers map[string]*pendingEntry
}

// NewClient initializes Spotify client using Authorization Code Flow.
func NewClient(opts ...Option) (*Client, error) {
	// Initialize client
	c := &Client{}

	// Apply options to override defaults
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// If a config wasn't provided or is invalid, create the default config
	if c.config == nil || c.config.Validate() != nil {
		config, err := config.NewConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create config: %w", err)
		}
		c.config = config
	}

	// Cloudflare Access credentials are passed to the worker client
	// The worker client attaches them as headers on every request
	c.workerClient = worker.NewClient(
		c.config.WorkerURL,
		c.config.CFAccessClientID,
		c.config.CFAccessClientSecret,
	)
	c.authenticatingUsers = make(map[string]*pendingEntry)

	return c, nil
}

// String returns a string representation of the client
func (c *Client) String() string {
	return "Spotify Client"
}

// -- Start/Stop ---

func (c *Client) Start() error {
	logger.Info("Spotify client started; auth triggers on first song request per user")
	return nil
}

func (c *Client) Stop() error {
	return nil
}

// SetMessenger sets the message sender for the client
func (c *Client) SetMessenger(messenger MessageSender) {
	c.messenger = messenger
}

// spotifyClientForUser creates a per-call Spotify SDK client for the given Discord user.
func (c *Client) spotifyClientForUser(userID string) *spotify.Client {
	t := &workerTransport{
		workerClient: c.workerClient,
		userID:       userID,
		base:         http.DefaultTransport,
	}
	return spotify.New(&http.Client{Transport: t})
}

// triggerAuthIfNeeded starts the OAuth flow in a background goroutine for the given user.
// If an auth flow is already running for this user, onSuccess is queued and will be called
// after auth completes. If auth fails, all queued callbacks are discarded and the user is
// notified to post their track again.
func (c *Client) triggerAuthIfNeeded(ctx context.Context, userID string, onSuccess func(context.Context)) {
	c.authMu.Lock()
	entry, exists := c.authenticatingUsers[userID]
	if exists {
		if onSuccess != nil {
			entry.callbacks = append(entry.callbacks, onSuccess)
		}
		c.authMu.Unlock()
		logger.Info("OAuth flow already in progress for user, queuing callback",
			zap.String(zapkey.UserID, userID))
		return
	}
	entry = &pendingEntry{}
	if onSuccess != nil {
		entry.callbacks = []func(context.Context){onSuccess}
	}
	c.authenticatingUsers[userID] = entry
	c.authMu.Unlock()

	go func() {
		authCtx := context.Background()
		if err := c.authenticate(authCtx, userID); err != nil {
			// Drain callbacks atomically with the map delete. We intentionally do not
			// call them: the original requests are no longer retryable (auth failed),
			// and calling them would re-enter the auth flow.
			c.authMu.Lock()
			delete(c.authenticatingUsers, userID)
			entry.callbacks = nil
			c.authMu.Unlock()

			logger.Error("Spotify OAuth flow failed", zap.Error(err), zap.String(zapkey.UserID, userID))
			c.reportToDiscord(authCtx, fmt.Sprintf(
				"❌ <@%s> Spotify authentication failed: %v\n"+
					"Post a track again to retry.", userID, err))
			return
		}

		// Drain all queued callbacks atomically with the map delete so no concurrent
		// caller can append to this entry after we have removed it.
		c.authMu.Lock()
		delete(c.authenticatingUsers, userID)
		callbacks := entry.callbacks
		entry.callbacks = nil
		c.authMu.Unlock()

		logger.Info("Spotify OAuth flow completed successfully", zap.String(zapkey.UserID, userID))
		if len(callbacks) == 0 {
			// Feedback for when the user has authenticated without any songs pending
			c.reportToDiscord(authCtx, fmt.Sprintf("✅ <@%s> Spotify connected successfully!", userID))
		} else {
			for _, cb := range callbacks {
				cb(authCtx)
			}
		}
	}()
}

// handleSpotifyError reports errors to Discord and re-triggers auth if needed.
func (c *Client) handleSpotifyError(ctx context.Context, err error, operation string, userID string) {
	if err == nil {
		return
	}
	if errors.Is(err, worker.ErrAuthRequired) {
		logger.Warn("Spotify auth required; triggering re-authentication",
			zap.Error(err), zap.String(zapkey.UserID, userID))
		c.reportToDiscord(ctx, fmt.Sprintf(
			"⚠️ <@%s> Spotify session expired or not connected. Re-authentication started — "+
				"check this channel for the login link.", userID))
		c.triggerAuthIfNeeded(ctx, userID, nil)
		return
	}
	logger.Error("Spotify operation failed",
		zap.String("operation", operation), zap.Error(err), zap.String(zapkey.UserID, userID))
	c.reportToDiscord(ctx, fmt.Sprintf("❌ <@%s> Spotify error (%s): %v", userID, operation, err))
}

func (c *Client) reportToDiscord(ctx context.Context, message string) {
	if c.messenger == nil {
		return
	}
	if err := c.messenger.SendMessage(ctx, channel.Auth.String(), message); err != nil {
		logger.Warn("Failed to post Spotify status to Discord", zap.Error(err))
	}
}
