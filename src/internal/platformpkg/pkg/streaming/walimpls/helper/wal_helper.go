//go:build extended
// +build extended

package helper

import (
	"go.uber.org/zap"

	"andb/platformpkg/pkg/log"
	"andb/platformpkg/pkg/streaming/util/types"
	"andb/platformpkg/pkg/streaming/walimpls"
)

// NewWALHelper creates a new WALHelper.
func NewWALHelper(opt *walimpls.OpenOption) *WALHelper {
	return &WALHelper{
		logger:  log.With(zap.String("channel", opt.Channel.String())),
		channel: opt.Channel,
	}
}

// WALHelper is a helper for WAL implementation.
type WALHelper struct {
	logger  *log.MLogger
	channel types.PChannelInfo
}

// Channel returns the channel of the WAL.
func (w *WALHelper) Channel() types.PChannelInfo {
	return w.channel
}

// Log returns the logger of the WAL.
func (w *WALHelper) Log() *log.MLogger {
	return w.logger
}
