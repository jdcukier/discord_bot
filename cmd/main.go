// Package main provides the entry point for the Discord bot
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/constants/zapkey"
	"discordbot/discord"
	"discordbot/log"
)

// main entry point for the application
func main() {
	// Initialize logger first
	defer log.Logger.Sync()

	// Load .env file
	if err := godotenv.Load(); err != nil {
		logger.Fatal("Failed to load .env file", zap.Error(err))
	}

	// Init and listen for HTTP requests
	discordClient, err := discord.NewClient()
	if err != nil {
		logger.Fatal("Failed to create Discord client", zap.Error(err))
	}
	err = discordClient.Start()
	if err != nil {
		logger.Fatal("Failed to start Discord client", zap.Error(err))
	}
	defer discordClient.Close()
	listen()
}

// listen starts the HTTP server and listens for incoming requests
func listen() {
	// Register the handler function for the default route
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/test", testEndpointHandler)

	// Start the server and listen on port 8080
	port := ":" + os.Getenv(envvar.Port)
	if port == ":" {
		port = ":8080"
	}
	logger.Info("Starting server", zap.String(zapkey.Port, port))
	err := http.ListenAndServe(port, nil) // The 'nil' uses the default ServeMux
	if err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
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
		logger.Error("Failed to write response", zap.Error(err))
	}
}

// healthHandler handles the health check route
func healthHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Health check")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "OK"); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}

// testEndpointHandler handles the test endpoint route
func testEndpointHandler(w http.ResponseWriter, r *http.Request) {
	appID := os.Getenv(envvar.AppID)
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "Test endpoint - APP_ID: %s", appID); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}
