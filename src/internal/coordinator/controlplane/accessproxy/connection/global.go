//go:build milvus_port
// +build milvus_port

package connection

import "sync"

var connectionManagerInstance *connectionManager

var getConnectionManagerInstanceOnce sync.Once

func GetManager() *connectionManager {
	getConnectionManagerInstanceOnce.Do(func() {
		connectionManagerInstance = newConnectionManager()
	})
	return connectionManagerInstance
}
