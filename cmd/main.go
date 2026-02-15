// Package main provides the entry point for the Discord bot
package main

import (
	"net/http"

	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()
	zap.ReplaceGlobals(logger)
	
	zap.S().Info("Starting server...")
	listen()
}

func listen() {
	// Register the handler function for the default route
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/test", testEndpointHandler)

	// Start the server and listen on port 8080
	port := ":8080"
	zap.S().Infof("Server starting on port %s\n", port)
	err := http.ListenAndServe(port, nil) // The 'nil' uses the default ServeMux
	if err != nil {
		zap.S().Fatal(err)
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	zap.S().Infof("Hello, World!")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello, World!"))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	zap.S().Infof("Health check")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func testEndpointHandler(w http.ResponseWriter, r *http.Request) {
	zap.S().Infof("Test endpoint")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Test endpoint"))
}
