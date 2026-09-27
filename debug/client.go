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

// HealthChecker reports whether a dependent service is healthy.
type HealthChecker interface {
	Healthy() bool
}

// Client for debugging this service
type Client struct {
	healthCheckers []HealthChecker
}

// NewClient creates a new debug client
func NewClient() (*Client, error) {
	return &Client{}, nil
}

// AddHealthChecker adds a health checker used by the /health endpoint.
// The service is healthy only when every added checker is healthy.
func (c *Client) AddHealthChecker(hc HealthChecker) {
	c.healthCheckers = append(c.healthCheckers, hc)
}

// healthy reports whether at least one checker is registered and all are healthy
func (c *Client) healthy() bool {
	if len(c.healthCheckers) == 0 {
		return false
	}
	for _, hc := range c.healthCheckers {
		if !hc.Healthy() {
			return false
		}
	}
	return true
}

func (c *Client) String() string {
	return "Debug Client"
}

// Start the debug client
func (c *Client) Start() error {
	// Register the handler function for the default route
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/health", c.healthHandler)
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

// healthHandler handles the health check route.
// Returns 200 if the health checker reports healthy, 503 otherwise.
func (c *Client) healthHandler(w http.ResponseWriter, r *http.Request) {
	if !c.healthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := fmt.Fprintf(w, "Discord not connected"); err != nil {
			logger.Error("Failed to write response", zap.Error(err), zap.String(zapkey.Path, r.URL.Path))
		}
		return
	}
	logger.Info("Health check")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "OK"); err != nil {
		logger.Error("Failed to write response", zap.Error(err), zap.String(zapkey.Path, r.URL.Path))
	}
}

// testEndpointHandler handles the test endpoint route
func testEndpointHandler(w http.ResponseWriter, r *http.Request) {
	key := envvar.Namespaced(envvar.DiscordAppID, envvar.NamespaceSpotify)
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "Test endpoint - %s: %s", key, os.Getenv(key)); err != nil {
		logger.Error("Failed to write response", zap.Error(err), zap.String(zapkey.Path, r.URL.Path))
	}
}
