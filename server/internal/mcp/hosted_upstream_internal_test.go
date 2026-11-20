package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestHostedAssignmentsDoNotBorrowSiblingRegistration(t *testing.T) {
	t.Parallel()
	project, issuer := uuid.New(), uuid.New()
	first, sibling, missing, noAuth, ambiguous := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	a, b := uuid.New(), uuid.New()
	row := func(server, registration uuid.UUID, requirement pgtype.Bool) upstreamclientsrepo.ListHostedUpstreamAssignmentsRow {
		return upstreamclientsrepo.ListHostedUpstreamAssignmentsRow{
			ServerID: server, ProjectID: project, UserSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true},
			RegistrationID: uuid.NullUUID{UUID: registration, Valid: registration != uuid.Nil}, RequiresUpstreamAuth: requirement,
		}
	}
	unknown, notRequired := pgtype.Bool{Bool: false, Valid: false}, pgtype.Bool{Bool: false, Valid: true}
	assignments := indexHostedAssignments([]upstreamclientsrepo.ListHostedUpstreamAssignmentsRow{
		row(first, a, unknown), row(sibling, b, unknown), row(missing, uuid.Nil, unknown), row(noAuth, uuid.Nil, notRequired),
		row(ambiguous, a, unknown), row(ambiguous, b, unknown),
	})
	for server, expected := range map[uuid.UUID]uuid.UUID{first: a, sibling: b, noAuth: uuid.Nil} {
		got, err := assignments[server].registration(project)
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	for _, server := range []uuid.UUID{missing, ambiguous, uuid.New()} {
		_, err := assignments[server].registration(project)
		require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
	}
	_, err := assignments[first].registration(uuid.New())
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
}
