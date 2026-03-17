//go:build extended
// +build extended

package client

import (
	"testing"

	"github.com/stretchr/testify/assert"

	kvfactory "github.com/milvus-io/milvus/internal/util/dependency/kv"
	"andb/platformpkg/pkg/util/paramtable"
)

func TestDial(t *testing.T) {
	paramtable.Init()

	c, _ := kvfactory.GetEtcdAndPath()
	assert.NotNil(t, c)

	client := NewClient(c)
	assert.NotNil(t, client)
	client.Close()
}
