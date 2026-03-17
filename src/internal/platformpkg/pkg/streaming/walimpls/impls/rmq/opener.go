//go:build extended
// +build extended

package rmq

import (
	"context"

	"andb/platformpkg/pkg/mq/mqimpl/rocksmq/client"
	"andb/platformpkg/pkg/streaming/util/types"
	"andb/platformpkg/pkg/streaming/walimpls"
	"andb/platformpkg/pkg/streaming/walimpls/helper"
)

var _ walimpls.OpenerImpls = (*openerImpl)(nil)

// openerImpl is the implementation of walimpls.Opener interface.
type openerImpl struct {
	c client.Client
}

// Open opens a new wal.
func (o *openerImpl) Open(ctx context.Context, opt *walimpls.OpenOption) (walimpls.WALImpls, error) {
	if err := opt.Validate(); err != nil {
		return nil, err
	}
	var p client.Producer
	if opt.Channel.AccessMode == types.AccessModeRW {
		var err error
		if p, err = o.c.CreateProducer(client.ProducerOptions{
			Topic: opt.Channel.Name,
		}); err != nil {
			return nil, err
		}
	}
	return &walImpl{
		WALHelper: helper.NewWALHelper(opt),
		p:         p,
		c:         o.c,
	}, nil
}

// Close closes the opener resources.
func (o *openerImpl) Close() {
	o.c.Close()
}
