package remotesessions_test

import (
	"encoding/json"
	"testing"

	project "github.com/speakeasy-api/gram/server/gen/http/remote_session_clients/server"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/stretchr/testify/require"
)

func TestPrepareEMAGrantsWire(t *testing.T) {
	t.Parallel()
	assertPreparationGrantWire(t, func(r *gen.IdentityChainingPreparation) any { return project.NewPrepareEMAResponseBody(r) })
}

func TestReadEMAGrantsWire(t *testing.T) {
	t.Parallel()
	assertPreparationGrantWire(t, func(r *gen.IdentityChainingPreparation) any { return project.NewReadEMAResponseBody(r) })
}

func TestUnlinkEMAGrantsWire(t *testing.T) {
	t.Parallel()
	assertPreparationGrantWire(t, func(r *gen.IdentityChainingPreparation) any { return project.NewUnlinkEMAResponseBody(r) })
}

func assertPreparationGrantWire(t *testing.T, encode func(*gen.IdentityChainingPreparation) any) {
	t.Helper()
	for _, tc := range []struct {
		name   string
		grants []string
		want   string
	}{
		{"unknown", nil, "null"},
		{"empty", []string{}, "[]"},
		{"populated", []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"}, `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := json.Marshal(encode(&gen.IdentityChainingPreparation{GrantTypes: tc.grants}))
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(wire, &fields))
			grants, present := fields["grant_types"]
			require.True(t, present)
			require.JSONEq(t, tc.want, string(grants))
		})
	}
}
