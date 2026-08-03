// Package ids generates entity identifiers.
package ids

import (
	"fmt"

	"github.com/google/uuid"
)

// New returns a UUIDv7. Its leading bits are a timestamp, so IDs sort by creation time,
// which keeps B-tree inserts append-mostly and lets us paginate on the ID alone.
func New() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil { // only fails when the system random source fails
		panic(fmt.Sprintf("ids: generate uuid: %v", err))
	}
	return id
}
