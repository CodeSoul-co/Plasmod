//go:build extended
// +build extended

package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"andb/platformpkg/pkg/streaming/util/types"
	"andb/platformpkg/pkg/streaming/walimpls"
)

func TestWALHelper(t *testing.T) {
	h := NewWALHelper(&walimpls.OpenOption{
		Channel: types.PChannelInfo{
			Name:       "test",
			Term:       1,
			AccessMode: types.AccessModeRW,
		},
	})
	assert.NotNil(t, h.Channel())
	assert.Equal(t, h.Channel().Name, "test")
	assert.Equal(t, h.Channel().AccessMode, types.AccessModeRW)
	assert.NotNil(t, h.Log())
}
