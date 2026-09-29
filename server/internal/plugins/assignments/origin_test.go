package assignments

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// All operations other than listing assignments panic: no-op and rejected
// mutations must never write an assignment, emit an audit, or load stale roles.
type originReadTx struct {
	pgx.Tx
	principals []string
}

func (tx originReadTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &originRows{principals: tx.principals}, nil
}

type originRows struct {
	pgx.Rows
	principals []string
	index      int
}

func (r *originRows) Next() bool { return r.index < len(r.principals) }
func (r *originRows) Close()     {}
func (r *originRows) Err() error { return nil }
func (r *originRows) Scan(dest ...any) error {
	principal, ok := dest[3].(*string)
	if !ok {
		return fmt.Errorf("unexpected principal destination: %T", dest[3])
	}
	*principal = r.principals[r.index]
	r.index++
	return nil
}
func originFixture() (pluginsrepo.Plugin, Input) {
	p := pluginsrepo.Plugin{ID: uuid.New(), OrganizationID: "org_test", ProjectID: uuid.New(), Name: "Role", Slug: "role"}
	return p, Input{OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, PluginID: p.ID, PrincipalURNs: []string{"role:organization:" + uuid.NewString()}, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "actor")}
}
func TestOriginNoOpPreservesStaleAudience(t *testing.T) {
	t.Parallel()
	for _, add := range []bool{true, false} {
		p, input := originFixture()
		current := []string{"role:organization:" + uuid.NewString(), "legacy-invalid-principal", "*"}
		if add {
			current = append(current, input.PrincipalURNs[0])
		}
		called := false
		guard := func(_ context.Context, _ pgx.Tx, _ pluginsrepo.Plugin, before, desired []string) error {
			called = true
			require.Equal(t, current, before)
			require.Equal(t, current, desired)
			return nil
		}
		result, err := mutateOrigin(t.Context(), originReadTx{principals: current}, audit.NewLogger(), p, input, guard, add)
		require.NoError(t, err)
		require.True(t, called)
		require.False(t, result.Changed)
		require.Equal(t, current, result.PrincipalURNs)
	}
}
func TestRemoveOriginGuardsCompleteAudienceWithoutRoleLookup(t *testing.T) {
	t.Parallel()
	p, input := originFixture()
	stale := "role:organization:" + uuid.NewString()
	current := []string{stale, input.PrincipalURNs[0], "*"}
	denied := errors.New("denied")
	guard := func(_ context.Context, _ pgx.Tx, _ pluginsrepo.Plugin, before, desired []string) error {
		require.Equal(t, current, before)
		require.Equal(t, []string{stale, "*"}, desired)
		return denied
	}
	_, err := RemoveOrigin(t.Context(), originReadTx{principals: current}, audit.NewLogger(), p, input, guard)
	require.ErrorIs(t, err, denied)
}
func TestOriginRequiresGuardAndExactlyOneRole(t *testing.T) {
	t.Parallel()
	p, input := originFixture()
	tx := originReadTx{}
	_, err := AddOrigin(t.Context(), tx, audit.NewLogger(), p, input, nil)
	require.ErrorIs(t, err, ErrInvalid)
	for _, principals := range [][]string{nil, {"*"}, {"user:member"}, {"role:organization:invalid"}, {input.PrincipalURNs[0], input.PrincipalURNs[0]}} {
		input.PrincipalURNs = principals
		_, err := RemoveOrigin(t.Context(), tx, audit.NewLogger(), p, input, LegacyGuard)
		require.ErrorIs(t, err, ErrInvalid)
	}
}

type missingRoleTx struct {
	originReadTx
	t *testing.T
}

func (tx missingRoleTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	require.Contains(tx.t, query, "GetOrganizationRoleByID")
	return missingRoleRow{}
}

type missingRoleRow struct{}

func (missingRoleRow) Scan(...any) error { return pgx.ErrNoRows }

func TestAddOriginValidatesOnlyNewRole(t *testing.T) {
	t.Parallel()
	p, input := originFixture()
	tx := missingRoleTx{originReadTx: originReadTx{principals: []string{"legacy-invalid-principal"}}, t: t}
	_, err := AddOrigin(t.Context(), tx, audit.NewLogger(), p, input, func(context.Context, pgx.Tx, pluginsrepo.Plugin, []string, []string) error {
		t.Fatal("invalid new role must fail before guard")
		return nil
	})
	require.ErrorIs(t, err, ErrInvalid)
}
