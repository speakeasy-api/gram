package database

import (
	"fmt"
	"hash/fnv"
	"math"
)

func LockIDForSyncWorkOSOrganization(organizationID string) (int64, error) {
	h := fnv.New64a()
	if _, err := h.Write([]byte("SyncWorkOSOrganization:")); err != nil {
		return 0, fmt.Errorf("write prefix to hash: %w", err)
	}
	if _, err := h.Write([]byte(organizationID)); err != nil {
		return 0, fmt.Errorf("write organization ID to hash: %w", err)
	}
	return int64(h.Sum64() & math.MaxInt64), nil
}
