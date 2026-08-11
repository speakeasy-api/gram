package chrepo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A nil pointer must collapse to an UNTYPED nil interface. The bug this guards
// against is a typed-nil (e.g. (*uuid.UUID)(nil)) reaching the ClickHouse
// driver as a non-nil interface: the driver then calls Value() on the nil
// pointer and panics. `got == nil` is true only for an untyped nil interface,
// so it distinguishes the safe case from the panicking typed-nil — which
// testify's require.Nil would not.
func TestChNullable_NilPointerBecomesUntypedNil(t *testing.T) {
	t.Parallel()

	var nilUUID *uuid.UUID
	gotUUID := chNullable(nilUUID)
	//nolint:testifylint // require.Nil passes for a typed-nil interface — the exact bug this guards against; only == nil detects an untyped nil.
	require.True(t, gotUUID == nil, "nil *uuid.UUID must bind as untyped nil, not a typed-nil interface")

	var nilTime *time.Time
	gotTime := chNullable(nilTime)
	//nolint:testifylint // see above: == nil is required to distinguish untyped nil from a panicking typed-nil.
	require.True(t, gotTime == nil, "nil *time.Time must bind as untyped nil")
}

func TestChNullable_NonNilPointerBecomesValue(t *testing.T) {
	t.Parallel()

	id := uuid.Must(uuid.NewV7())
	gotUUID := chNullable(&id)
	require.NotNil(t, gotUUID)
	require.Equal(t, id, gotUUID, "non-nil pointer must bind as its dereferenced value")

	ts := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	gotTime := chNullable(&ts)
	require.NotNil(t, gotTime)
	require.Equal(t, ts, gotTime)
}
