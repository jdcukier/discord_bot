package log

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactID(t *testing.T) {
	assert.Equal(t, "37i9…", RedactID("37i9dQZF1DXcBWIGoYBM5M"))
	assert.Equal(t, "abcd", RedactID("abcd"))
	assert.Equal(t, "", RedactID(""))
	assert.Equal(t, []string{"0123…", "ab"}, RedactIDs([]string{"0123456789", "ab"}))
}
