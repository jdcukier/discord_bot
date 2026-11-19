package log

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactID(t *testing.T) {
	assert.Equal(t, "37i9…", RedactID("37i9dQZF1DXcBWIGoYBM5M"))
	assert.Equal(t, "abcd", RedactID("abcd"))
	assert.Equal(t, "", RedactID(""))
	assert.Equal(t, []string{"0123…", "ab"}, RedactIDs([]string{"0123456789", "ab"}))
}

func TestRedactErr(t *testing.T) {
	assert.NoError(t, RedactErr(nil, "x"))

	base := errors.New("base")
	err := fmt.Errorf(`Get "https://api.spotify.com/v1/playlists/0123456789abcdefghijkl/tracks": %w`, base)
	redacted := RedactErr(err, "0123456789abcdefghijkl", "")
	assert.Equal(t, `Get "https://api.spotify.com/v1/playlists/0123…/tracks": base`, redacted.Error())
	assert.ErrorIs(t, redacted, base)

	// Unchanged errors are returned as is
	assert.Same(t, base, RedactErr(base, "0123456789abcdefghijkl"))
}

func TestRedactMatches(t *testing.T) {
	pattern := regexp.MustCompile(`[0-9A-Za-z]{22,}`)
	base := errors.New("base")
	err := fmt.Errorf(`invalid tag name "0123456789abcdefghijkl" and ABCDEFGHIJKLMNOPQRSTUV: %w`, base)
	redacted := RedactMatches(err, pattern)
	assert.Equal(t, `invalid tag name "0123…" and ABCD…: base`, redacted.Error())
	assert.ErrorIs(t, redacted, base)

	assert.Same(t, base, RedactMatches(base, pattern))
	assert.NoError(t, RedactMatches(nil, pattern))
}
