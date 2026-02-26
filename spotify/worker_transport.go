package spotify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"discordbot/spotify/worker"
)

// workerTransport is an http.RoundTripper that authenticates Spotify API requests
// using access tokens fetched from the Cloudflare Worker.
type workerTransport struct {
	workerClient *worker.Client
	userID       string
	base         http.RoundTripper

	mu    sync.Mutex
	cache *worker.TokenData
}

func (t *workerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so it can be replayed if a token refresh forces a retry.
	var bodyBytes []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading request body for retry buffer: %w", err)
		}
		bodyBytes = b
	}

	token, err := t.cachedToken(req.Context())
	if err != nil {
		return nil, err
	}

	req = req.Clone(req.Context())
	if bodyBytes != nil {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()

		fresh, refreshErr := t.forceRefreshAndCache(req.Context())
		if refreshErr != nil {
			if errors.Is(refreshErr, worker.ErrAuthRequired) {
				return nil, refreshErr
			}
			return nil, fmt.Errorf("force-refresh after 401: %w", refreshErr)
		}

		req = req.Clone(req.Context())
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		req.Header.Set("Authorization", "Bearer "+fresh.AccessToken)
		return t.base.RoundTrip(req)
	}

	return resp, nil
}

func (t *workerTransport) cachedToken(ctx context.Context) (*worker.TokenData, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cache != nil && !t.cache.IsExpired(0) {
		return t.cache, nil
	}

	token, err := t.workerClient.GetToken(ctx, t.userID)
	if err != nil {
		return nil, fmt.Errorf("fetching token from worker: %w", err)
	}

	t.cache = token
	return token, nil
}

func (t *workerTransport) forceRefreshAndCache(ctx context.Context) (*worker.TokenData, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cache = nil
	fresh, err := t.workerClient.ForceRefresh(ctx, t.userID)
	if err != nil {
		return nil, err
	}
	t.cache = fresh
	return fresh, nil
}

// ClearCache invalidates the in-memory token cache so the next request fetches
// a fresh token from the worker. Called after successful re-authentication.
func (t *workerTransport) ClearCache() {
	t.mu.Lock()
	t.cache = nil
	t.mu.Unlock()
}
