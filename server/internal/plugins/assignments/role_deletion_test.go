package assignments

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRemoveDeletedRoleRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	for _, input := range []RoleDeletion{
		{OrganizationID: "", PrincipalURN: "role:organization:" + id},
		{OrganizationID: "org_cleanup", PrincipalURN: "role:global:" + id},
		{OrganizationID: "org_cleanup", PrincipalURN: "*"},
		{OrganizationID: "org_cleanup", PrincipalURN: "user:" + id},
		{OrganizationID: "org_cleanup", PrincipalURN: "role:organization:not-a-uuid"},
		{OrganizationID: "org_cleanup", PrincipalURN: "role:organization:" + uuid.Nil.String()},
		{OrganizationID: "org_cleanup", PrincipalURN: "role:unknown:" + id},
	} {
		input.Actor = urn.NewSystemPrincipal("workos-role-sync")
		// Invalid scope must be rejected before any database access.
		require.ErrorIs(t, RemoveDeletedRole(t.Context(), nil, audit.NewLogger(), input), ErrInvalid, "%+v", input)
	}
}
