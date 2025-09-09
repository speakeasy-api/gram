package must

import "github.com/google/uuid"

// UUID returns the UUID if valid, otherwise panics.
func UUID(v uuid.NullUUID) uuid.UUID {
	if !v.Valid {
		panic("uuid is not valid")
	}
	return v.UUID
}
