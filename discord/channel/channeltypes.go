// Package channel contains utilities for managing channels
package channel

const (
	// Auth type channel for sending Authentication URLs
	Auth Type = "Authentication"

	// Songs type channel for collecting songs to a playlist
	Songs Type = "Songs"

	// Debug type channel for debugging this service
	Debug Type = "Debug"
)

// Type represents a channel type
type Type string

// NewType creates a new channel type from a string
func NewType(s string) Type {
	return Type(s)
}

// String returns the string representation of the channel type
func (t Type) String() string {
	return string(t)
}
