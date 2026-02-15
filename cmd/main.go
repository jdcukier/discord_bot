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

func listen() {
	// Register the handler function for the default route
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/test", testEndpointHandler)
	http.HandleFunc("/interactions", interactionsHandler)

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

func homeHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Hello, World!", zap.String(zapkey.Path, r.URL.Path))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello, World!"))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Health check")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func testEndpointHandler(w http.ResponseWriter, r *http.Request) {
	appID := os.Getenv(envvar.AppID)
	w.WriteHeader(http.StatusOK)
	logger.Info("Test endpoint", zap.String(zapkey.AppID, appID))
	w.Write([]byte(fmt.Sprintf("Test endpoint - APP_ID: %s", appID)))
}

func interactionsHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Interactions endpoint", zap.String(zapkey.Method, r.Method), zap.String(zapkey.Path, r.URL.Path))
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for interactions endpoint", zap.String(zapkey.Method, r.Method))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	handler, err := discord.NewInteractionHandler(r)
	if err != nil {
		logger.Error("Failed to create interaction handler", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	handler.Handle(w)
}

