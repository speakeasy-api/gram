//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

// TestSeededLegacyAssistantProvisions guards the seeded assistant trigger
// trust registration: provisioning the legacy assistant must reuse it rather
// than fail on its name, and both seeded assistants then resolve as active.
func TestSeededLegacyAssistantProvisions(t *testing.T) {
	t.Parallel()

	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgID, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			seedLocalPostgres(ctx, t, db, spec)

			s := &seeder{spec: spec}
			project := s.projectID()
			bound, legacy := s.det("assistant-bound"), s.det("assistant-legacy")

			identities := assistantidentity.New(testAssistantIssuerURL+"/", audit.NewLogger())
			require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
				return identities.Provision(ctx, tx, assistantidentity.ProvisionParams{
					OrganizationID: spec.OrgID,
					ProjectID:      project,
					AssistantID:    legacy,
					ActorUserID:    s.userID(demoUsers[0].handle),
					Staff:          nil,
				})
			}))

			states, err := assistantidentity.States(ctx, db, project, []uuid.UUID{bound, legacy})
			require.NoError(t, err)
			require.Equal(t, assistantidentity.Active, states[bound].State)
			require.Equal(t, assistantidentity.Active, states[legacy].State)

			issuers, err := identityrepo.New(db).FindProjectIssuer(ctx, identityrepo.FindProjectIssuerParams{
				OrganizationID: spec.OrgID,
				ProjectID:      uuid.NullUUID{UUID: project, Valid: true},
				Issuers:        []string{testAssistantIssuerURL},
			})
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{s.det("assistant-platform-trust")}, issuers)
		})
	}
}
