package spotify

import (
	"context"
	"discordbot/constants/envvar"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zmb3/spotify/v2"
	auth "github.com/zmb3/spotify/v2/auth"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const (
	state = "discord-bot-state" // TODO: Make this randomized
)

// Client represents a spotify client
type Client struct {
	api           *spotify.Client
	authenticator *auth.Authenticator
	tokenChan     chan *oauth2.Token
}

// NewClient initializes Spotify client using Authorization Code Flow.
func NewClient() (*Client, error) {
	client := &Client{
		tokenChan: make(chan *oauth2.Token, 1),
	}

	return client, nil
}

// String returns a string representation of the client
func (c *Client) String() string {
	return "Spotify Client"
}

// -- Start/Stop ---

func (c *Client) Start() error {
	// Register handlers
	http.HandleFunc("/spotify/callback", c.CallbackHandler())

	// Grab env vars
	clientID := os.Getenv(envvar.SpotifyID)
	clientSecret := os.Getenv(envvar.SpotifySecret)
	redirectURI := os.Getenv(envvar.SpotifyRedirectURI)

	// Check for missing env vars
	var missing []string
	if clientID == "" {
		missing = append(missing, envvar.SpotifyID)
	}
	if clientSecret == "" {
		missing = append(missing, envvar.SpotifySecret)
	}

	if redirectURI == "" {
		missing = append(missing, envvar.SpotifyRedirectURI)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing env vars: %s", strings.Join(missing, ", "))
	}

	// Initialize authenticator
	c.authenticator = auth.New(
		auth.WithRedirectURL(redirectURI),
		auth.WithScopes(
			auth.ScopePlaylistModifyPublic,
			auth.ScopePlaylistModifyPrivate,
		),
		auth.WithClientID(clientID),
		auth.WithClientSecret(clientSecret),
	)

	// Load token
	token, err := loadToken()
	if err != nil {
		// No token saved yet — start auth flow
		token, err = c.authenticate()
		if err != nil {
			return fmt.Errorf("failed to authenticate spotify client: %w", err)
		}
		if err := saveToken(token); err != nil {
			return fmt.Errorf("failed to save spotify token: %w", err)
		}
	}

	// Initialize API
	httpClient := c.authenticator.Client(context.Background(), token)
	c.api = spotify.New(httpClient)

	return nil
}

func (c *Client) Stop() error {
	return nil
}

// -- Tracks ---

func (c *Client) AddTrackToPlaylist(
	ctx context.Context,
	playlistID string,
	trackID string,
) error {
	_, err := c.api.AddTracksToPlaylist(
		ctx,
		spotify.ID(playlistID),
		spotify.ID(trackID),
	)
	return err
}

// --- Auth Flow ---

// Authenticate using Authorization Code Flow
func (c *Client) authenticate() (*oauth2.Token, error) {
	authURL := c.authenticator.AuthURL(state)
	fmt.Println("Open this URL in your browser to authenticate:")
	fmt.Println(authURL)

	// Wait for callback
	select {
	case token := <-c.tokenChan:
		return token, nil
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timeout")
	}
}

// CallbackHandler returns the HTTP handler for the Spotify OAuth callback
func (c *Client) CallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("state") != state {
			http.Error(w, "State mismatch", http.StatusForbidden)
			return
		}

		token, err := c.authenticator.Token(r.Context(), state, r)
		if err != nil {
			http.Error(w, "Couldn't get token", http.StatusForbidden)
			return
		}

		if _, err := fmt.Fprintln(w, "Spotify authentication successful! You can close this window."); err != nil {
			logger.Error("Failed to write response", zap.Error(err))
		}

		select {
		case c.tokenChan <- token:
		default:
			// Channel is full or closed, ignore
		}
	}
}

// --- Token Storage ---
// TODO: Expand this and/or use a DB

// Cached token
var token *oauth2.Token

// Save token to memory
func saveToken(newToken *oauth2.Token) error {
	if newToken == nil {
		return fmt.Errorf("token is nil")
	}
	token = newToken
	return nil
}

// Load token from memory
func loadToken() (*oauth2.Token, error) {
	if token == nil {
		return nil, fmt.Errorf("token is nil")
	}
	return token, nil
}
