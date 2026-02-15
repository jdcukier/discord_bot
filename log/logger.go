// Package log provides a global logger for the project
package log

import (
	"go.uber.org/zap"
)

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