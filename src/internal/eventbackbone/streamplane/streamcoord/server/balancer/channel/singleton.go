//go:build extended
// +build extended

package channel

import (
	"andb/platformpkg/pkg/streaming/util/message"
	"andb/platformpkg/pkg/util/syncutil"
)

var singleton = syncutil.NewFuture[*ChannelManager]()

// register sets the global ChannelManager singleton.
func register(cm *ChannelManager) {
	singleton.Set(cm)
}

// GetClusterChannelsOpt is a functional option for GetClusterChannels.
type GetClusterChannelsOpt func(*getClusterChannelsOptions)

type getClusterChannelsOptions struct {
	includeUnavailableInReplication bool
}

// OptIncludeUnavailableInReplication includes channels that are unavailable in replication.
func OptIncludeUnavailableInReplication() GetClusterChannelsOpt {
	return func(o *getClusterChannelsOptions) {
		o.includeUnavailableInReplication = true
	}
}

// GetClusterChannels blocks until the ChannelManager is registered,
// then returns the cluster channel topology.
// By default, only channels available in replication are returned.
// Use OptIncludeUnavailableInReplication() to include unavailable channels.
func GetClusterChannels(opts ...GetClusterChannelsOpt) message.ClusterChannels {
	return singleton.Get().getClusterChannels(opts...)
}
