//go:build test
// +build test

package registry

import "andb/platformpkg/pkg/util/syncutil"

func ResetRegisterLocalWALManager() {
	registry = syncutil.NewFuture[WALManager]()
}
