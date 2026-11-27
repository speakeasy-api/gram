package orgserving_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgserving"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	disabledAt := pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}
	for _, tc := range []struct {
		name         string
		organization orgrepo.OrganizationMetadatum
		want         orgserving.Refusal
	}{
		{name: "served", organization: orgrepo.OrganizationMetadatum{Whitelisted: true}, want: orgserving.RefusalNone},
		{name: "not_whitelisted", organization: orgrepo.OrganizationMetadatum{Whitelisted: false}, want: orgserving.RefusalNotWhitelisted},
		{name: "disabled", organization: orgrepo.OrganizationMetadatum{Whitelisted: true, DisabledAt: disabledAt}, want: orgserving.RefusalDisabled},
		{name: "disabled_wins", organization: orgrepo.OrganizationMetadatum{Whitelisted: false, DisabledAt: disabledAt}, want: orgserving.RefusalDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, orgserving.Check(tc.organization))
		})
	}
}
