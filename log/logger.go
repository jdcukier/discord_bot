// Package log provides a global logger for the project
package log

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
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

// redactedError hides IDs in an error's message but keeps the original for errors.Is/As
type redactedError struct {
	err error
	msg string
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// RedactErr returns err with every occurrence of the IDs in its message shortened by RedactID.
// Errors from HTTP clients often include the request URL, and with it the IDs in its path.
func RedactErr(err error, ids ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, id := range ids {
		if id != "" {
			msg = strings.ReplaceAll(msg, id, RedactID(id))
		}
	}
	if msg == err.Error() {
		return err
	}
	return &redactedError{err: err, msg: msg}
}

// RedactMatches returns err with every match of pattern in its message shortened by RedactID,
// for errors that may contain IDs without knowing which ones
func RedactMatches(err error, pattern *regexp.Regexp) error {
	if err == nil {
		return nil
	}
	msg := pattern.ReplaceAllStringFunc(err.Error(), RedactID)
	if msg == err.Error() {
		return err
	}
	return &redactedError{err: err, msg: msg}
}
