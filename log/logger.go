// Package log provides a global logger for the project
package log

import (
	"context"
	"os"
	"strconv"
	"unicode/utf8"

	"go.uber.org/zap"

	"discordbot/constants/envvar"
	"discordbot/utils/ctxutil"
)

// Logger is the global logger, used to derive package loggers
var Logger = initLogger()

func initLogger() *zap.Logger {
	var err error
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)
	return logger
}

// VerboseLogsEnabled returns true if verbose logs are enabled
func VerboseLogsEnabled(ctx context.Context) bool {
	val := os.Getenv(envvar.VerboseLogsEnabled)
	if val == "" {
		return false
	}
	verboseLogsEnabled, err := strconv.ParseBool(val)
	if err != nil {
		fields := ctxutil.ZapFields(ctx)
		Logger.With(zap.Error(err)).Warn("Failed to parse verbose logs enabled", fields...)
	}
	return verboseLogsEnabled
}

// redactedIDLength is how many leading characters of an ID are kept in logs
const redactedIDLength = 4

// RedactID shortens an ID for logging, e.g. "37i9dQZF1DXcBWIGoYBM5M" becomes "37i9…".
// Deploy health checks publish recent logs on the public repo, so private IDs
// such as playlist IDs must not appear in full.
func RedactID(id string) string {
	if utf8.RuneCountInString(id) <= redactedIDLength {
		return id
	}
	return string([]rune(id)[:redactedIDLength]) + "…"
}

// RedactIDs applies RedactID to each ID
func RedactIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = RedactID(id)
	}
	return out
}
