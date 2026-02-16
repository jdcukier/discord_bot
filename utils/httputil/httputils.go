package httputil

import (
	"os"

	"discordbot/constants/envvar"
)

// Port returns the port number to listen on from the environment, or :8080 if not set
func Port() string {
	port := ":" + os.Getenv(envvar.Port)
	if port == ":" {
		port = ":8080"
	}
	return port
}
