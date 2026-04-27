//go:build test
// +build test

package registry

import "plasmod/platformpkg/pkg/util/syncutil"

func ResetRegisterLocalWALManager() {
	registry = syncutil.NewFuture[WALManager]()
}
