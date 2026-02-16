// Package ctxutil provides utilities for injecting and retrieving metadata via go context.
package ctxutil

import (
	"context"

	"go.uber.org/zap"
)

// Define a unique key type for storing the zap fields in the context
type zapFieldsKeyType struct{}

// Instantiate the unique key for the zap fields in the context
// Note: This MUST be unexported to ensure that it is not accessible from other packages
var zapFieldsKey = zapFieldsKeyType{}

// WithZapFields injects the zap fields into the context
func WithZapFields(ctx context.Context, fields ...zap.Field) (context.Context, []zap.Field) {
	if ctx == nil {
		ctx = context.Background()
	}
	existingFields := ZapFields(ctx)
	fields = append(fields, existingFields...)
	return context.WithValue(ctx, zapFieldsKey, fields), fields
}

// ZapFields retrieves the zap fields from the context
func ZapFields(ctx context.Context) []zap.Field {
	if ctx == nil {
		return nil
	}
	value := ctx.Value(zapFieldsKey)
	if val, ok := value.([]zap.Field); ok && val != nil {
		return val
	}
	return []zap.Field{}
}
