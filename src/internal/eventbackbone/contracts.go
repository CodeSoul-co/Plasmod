package eventbackbone

import "andb/src/internal/schemas"

type WAL interface {
	Append(event schemas.Event) (WALEntry, error)
}

type Bus interface {
	Subscribe(channel string) <-chan Message
	Publish(msg Message)
}
