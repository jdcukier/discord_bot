// Package log provides a global logger for the project
package log

import (
	"context"
	"os"
	"strconv"

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
	verboseLogsEnabled, err := strconv.ParseBool(os.Getenv(envvar.VerboseLogsEnabled))
	if err != nil {
		fields := ctxutil.ZapFields(ctx)
		Logger.With(zap.Error(err)).Warn("Failed to parse verbose logs enabled", fields...)
	}
	return verboseLogsEnabled
}
