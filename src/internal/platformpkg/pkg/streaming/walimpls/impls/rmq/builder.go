//go:build extended
// +build extended

package rmq

import (
	"plasmod/platformpkg/pkg/mq/mqimpl/rocksmq/client"
	"plasmod/platformpkg/pkg/mq/mqimpl/rocksmq/server"
	"plasmod/platformpkg/pkg/streaming/util/message"
	"plasmod/platformpkg/pkg/streaming/walimpls"
	"plasmod/platformpkg/pkg/streaming/walimpls/registry"
)

func init() {
	// register the builder to the registry.
	registry.RegisterBuilder(&builderImpl{})
	// register the unmarshaler to the message registry.
	message.RegisterMessageIDUnmsarshaler(message.WALNameRocksmq, UnmarshalMessageID)
}

// builderImpl is the builder for rmq opener.
type builderImpl struct{}

// Name of the wal builder, should be a lowercase string.
func (b *builderImpl) Name() message.WALName {
	return message.WALNameRocksmq
}

// Build build a wal instance.
func (b *builderImpl) Build() (walimpls.OpenerImpls, error) {
	c, err := client.NewClient(client.Options{
		Server: server.Rmq,
	})
	if err != nil {
		return nil, err
	}
	return &openerImpl{
		c: c,
	}, nil
}
