//go:build milvus_port
// +build milvus_port

package common

import semver "github.com/blang/semver/v4"

// Version current version for session
var Version semver.Version

func init() {
	Version = semver.MustParse("2.6.6")
}
