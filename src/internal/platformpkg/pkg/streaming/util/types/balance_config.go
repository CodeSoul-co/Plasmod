//go:build extended
// +build extended

package types

import "plasmod/platformpkg/pkg/proto/streamingpb"

const (
	UpdateMaskPathWALBalancePolicyAllowRebalance = "config.allow_rebalance"
)

type (
	UpdateWALBalancePolicyRequest  = streamingpb.UpdateWALBalancePolicyRequest
	UpdateWALBalancePolicyResponse = streamingpb.UpdateWALBalancePolicyResponse
)
