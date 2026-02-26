// Package worker provides an HTTP client for the banger-spotify-worker Cloudflare Worker API.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrAuthRequired is returned when no token exists for the user (HTTP 404 from worker),
// or when the refresh token is invalid (HTTP 502 from worker, meaning Spotify rejected it).
// The caller should trigger the full OAuth flow on receiving this error.
var ErrAuthRequired = errors.New("spotify authentication required")

// TokenData is the token structure stored/returned by the worker.
// expires_at is a Unix timestamp set by the worker at write time.
type TokenData struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope"`
}

// IsExpired returns true if the token is expired or will expire within buffer.
func (t *TokenData) IsExpired(buffer time.Duration) bool {
	return time.Now().Add(buffer).Unix() >= t.ExpiresAt
}

// Client is an HTTP client for the Cloudflare Worker API.
// Authentication is via Cloudflare Access service token headers.
type Client struct {
	baseURL              string
	cfAccessClientID     string
	cfAccessClientSecret string
	httpClient           *http.Client
}

// NewClient creates a new worker API client.
func NewClient(baseURL, cfClientID, cfClientSecret string) *Client {
	return &Client{
		baseURL:              baseURL,
		cfAccessClientID:     cfClientID,
		cfAccessClientSecret: cfClientSecret,
		httpClient:           &http.Client{Timeout: 10 * time.Second},
	}
}

// do executes a request with CF Access service token headers attached.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("CF-Access-Client-Id", c.cfAccessClientID)
	req.Header.Set("CF-Access-Client-Secret", c.cfAccessClientSecret)
	return c.httpClient.Do(req)
}

// GetAuthURL fetches a signed Spotify OAuth URL from the worker for userID.
// The bot posts this URL to Discord; after the user clicks it, the worker handles
// the OAuth callback and stores the token in KV.
func (c *Client) GetAuthURL(ctx context.Context, userID string) (string, error) {
	u, err := url.Parse(c.baseURL + "/auth-url")
	if err != nil {
		return "", fmt.Errorf("parsing auth-url endpoint: %w", err)
	}
	q := u.Query()
	q.Set("user_id", userID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("building auth-url request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("auth-url request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth-url returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AuthURL string `json:"auth_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding auth-url response: %w", err)
	}
	return result.AuthURL, nil
}

// GetToken fetches the token for userID from the worker.
// The worker auto-refreshes if within 60s of expiry.
// Returns ErrAuthRequired if no token exists yet (HTTP 404).
func (c *Client) GetToken(ctx context.Context, userID string) (*TokenData, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		fmt.Sprintf("%s/token/%s", c.baseURL, url.PathEscape(userID)),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("building get-token request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("get-token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrAuthRequired // No token yet; caller should trigger OAuth flow
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get-token returned %d: %s", resp.StatusCode, body)
	}

	var token TokenData
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &token, nil
}

// ForceRefresh forces the worker to refresh the token for userID.
// Returns ErrAuthRequired if the refresh token is invalid (Spotify rejected it),
// indicating the full OAuth flow must be re-triggered.
func (c *Client) ForceRefresh(ctx context.Context, userID string) (*TokenData, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		fmt.Sprintf("%s/refresh/%s", c.baseURL, url.PathEscape(userID)),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("building force-refresh request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("force-refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// 404: token was deleted (race); 502: Spotify rejected the refresh_token (revoked)
		// Both require re-authentication.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadGateway {
			return nil, fmt.Errorf("%w: force-refresh returned %d: %s",
				ErrAuthRequired, resp.StatusCode, body)
		}
		return nil, fmt.Errorf("force-refresh returned %d: %s", resp.StatusCode, body)
	}

	var token TokenData
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decoding refresh response: %w", err)
	}
	return &token, nil
}
