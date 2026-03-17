//go:build extended
// +build extended

package walimplstest

import (
	"testing"

	"andb/platformpkg/pkg/streaming/walimpls"
)

func TestWALImplsTest(t *testing.T) {
	enableFenceError.Store(false)
	defer enableFenceError.Store(true)
	walimpls.NewWALImplsTestFramework(t, 100, &openerBuilder{}).Run()
}
