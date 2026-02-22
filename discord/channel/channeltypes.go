// Package channel contains utilities for managing channels
package channel

const (
	// Auth type channel for sending Authentication URLs
	Auth Type = "Authentication"

	// Debug type channel for debugging this service
	Debug Type = "Debug"
)

// Type represents a channel type
type Type string

// String returns the string representation of the channel type
func (t Type) String() string {
	return string(t)
}
