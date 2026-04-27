//go:build extended
// +build extended

package ce

import (
	"plasmod/platformpkg/pkg/proto/messagespb"
	"plasmod/platformpkg/pkg/streaming/util/message"
)

func NewBuilder() *CacheExpirationsBuilder {
	return &CacheExpirationsBuilder{
		cacheExpirations: &message.CacheExpirations{
			CacheExpirations: make([]*message.CacheExpiration, 0, 1),
		},
	}
}

type CacheExpirationsBuilder struct {
	cacheExpirations *message.CacheExpirations
}

func (b *CacheExpirationsBuilder) WithLegacyProxyCollectionMetaCache(opts ...OptLegacyProxyCollectionMetaCache) *CacheExpirationsBuilder {
	lpcmc := &message.LegacyProxyCollectionMetaCache{}
	for _, opt := range opts {
		opt(lpcmc)
	}
	b.cacheExpirations.CacheExpirations = append(b.cacheExpirations.CacheExpirations, &message.CacheExpiration{
		Cache: &messagespb.CacheExpiration_LegacyProxyCollectionMetaCache{
			LegacyProxyCollectionMetaCache: lpcmc,
		},
	})
	return b
}

func (b *CacheExpirationsBuilder) Build() *message.CacheExpirations {
	return b.cacheExpirations
}
