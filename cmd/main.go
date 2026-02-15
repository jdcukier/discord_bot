// Package main provides the entry point for the Discord bot
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/discord"
)

func main() {
	// Initialize logger first
	initLogger()

	// Load .env file
	err := godotenv.Load()
	if err != nil {
		zap.L().Fatal("Failed to load .env file")
	}

	// Init and listen for HTTP requests
	discordClient, err := discord.NewClient()
	if err != nil {
		zap.L().Fatal("Failed to create Discord client", zap.Error(err))
	}
	err = discordClient.Start()
	if err != nil {
		zap.L().Fatal("Failed to start Discord client", zap.Error(err))
	}
	defer discordClient.Close()
	listen()
}

func initLogger() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()
	zap.ReplaceGlobals(logger)
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
	zap.L().Info("Starting server", zap.String("port", port))
	err := http.ListenAndServe(port, nil) // The 'nil' uses the default ServeMux
	if err != nil {
		zap.L().Fatal("Failed to start server", zap.Error(err))
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	zap.L().Info("Hello, World!", zap.String("path", r.URL.Path))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello, World!"))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	zap.L().Info("Health check")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func testEndpointHandler(w http.ResponseWriter, r *http.Request) {
	appID := os.Getenv(envvar.AppID)
	w.WriteHeader(http.StatusOK)
	zap.L().Info("Test endpoint", zap.String("app_id", appID))
	w.Write([]byte(fmt.Sprintf("Test endpoint - APP_ID: %s", appID)))
}

func interactionsHandler(w http.ResponseWriter, r *http.Request) {
	zap.L().Info("Interactions endpoint", zap.String("method", r.Method), zap.String("path", r.URL.Path))
	
	if r.Method != http.MethodPost {
		zap.L().Warn("Invalid method for interactions endpoint", zap.String("method", r.Method))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	handler, err := discord.NewInteractionHandler(r)
	if err != nil {
		zap.L().Error("Failed to create interaction handler", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	handler.Handle(w)
}

