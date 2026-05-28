package risk_analysis

import (
	"time"

	"github.com/google/uuid"
)

// UUIDv7LowerBound returns the smallest possible UUIDv7 at or after t.
// All random bits are zeroed so any real UUIDv7 generated at time t sorts
// >= this value, making it safe to use in WHERE id >= @lower_bound clauses
// that leverage the (project_id, id) index as a time-range filter without
// a separate created_at index.
func UUIDv7LowerBound(t time.Time) uuid.UUID {
	ms := uint64(t.UnixMilli())
	var u uuid.UUID
	u[0] = byte(ms >> 40) //nolint:gosec // intentional bit extraction; high bits always zero for valid timestamps
	u[1] = byte(ms >> 32) //nolint:gosec // intentional bit extraction; high bits always zero for valid timestamps
	u[2] = byte(ms >> 24) //nolint:gosec // intentional bit extraction; high bits always zero for valid timestamps
	u[3] = byte(ms >> 16) //nolint:gosec // intentional bit extraction; high bits always zero for valid timestamps
	u[4] = byte(ms >> 8)  //nolint:gosec // intentional bit extraction; high bits always zero for valid timestamps
	u[5] = byte(ms)       //nolint:gosec // intentional bit extraction; high bits always zero for valid timestamps
	u[6] = 0x70           // version=7 in top 4 bits, rand_a=0
	u[8] = 0x80           // RFC 4122 variant in top 2 bits, rand_b=0
	return u
}
