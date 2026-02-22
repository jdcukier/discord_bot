// Package debug provides utilities for debugging this service
package debug

import (
	"fmt"
	"net/http"
	"os"

	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
)

// Client for debugging this service
type Client struct{}

// NewClient creates a new debug client
func NewClient() (*Client, error) {
	return &Client{}, nil
}

func (c *Client) String() string {
	return "Debug Client"
}

// Start the debug client
func (c *Client) Start() error {
	// Register the handler function for the default route
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/test", testEndpointHandler)
	return nil
}

// Stop the debug client
func (c *Client) Stop() error {
	return nil
}

// homeHandler handles the default route
func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// Not our job to handle this
		return
	}
	logger.Info("Hello, World!", zap.String(zapkey.Path, r.URL.Path))
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "Hello, World!"); err != nil {
		logger.Error("Failed to write response", zap.Error(err), zap.String(zapkey.Path, r.URL.Path))
	}
}

// healthHandler handles the health check route
func healthHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Health check")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "OK"); err != nil {
		logger.Error("Failed to write response", zap.Error(err), zap.String(zapkey.Path, r.URL.Path))
	}
}

// testEndpointHandler handles the test endpoint route
func testEndpointHandler(w http.ResponseWriter, r *http.Request) {
	appID := os.Getenv(envvar.DiscordAppID)
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "Test endpoint - DISCORD_APP_ID: %s", appID); err != nil {
		logger.Error("Failed to write response", zap.Error(err), zap.String(zapkey.Path, r.URL.Path))
	}
}
