package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testSpans = `[{"match": "AKIAIOSFODNN7EXAMPLE", "field": "content", "path": "", "start_pos": 0, "end_pos": 20}]`

func provisionRiskEvidence(t *testing.T, fixture *orgencryptiontest.Fixture, organizationID string) {
	t.Helper()
	require.NoError(t, fixture.Provisioner.EnsureUseCase(t.Context(), organizationID, orgencryption.UseCaseRiskEvidence, orgencryption.SystemActor()))
}

func TestResultEvidenceCipher_SealKeepsPlaintextUntilProvisioned(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	fixture := orgencryptiontest.New(t, ti.conn)
	cipher := risk.NewResultEvidenceCipher(fixture.Service)
	match := pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true}

	plaintext, err := cipher.Seal(ctx, orgID, match, []byte(testSpans))
	require.NoError(t, err)
	require.Equal(t, match, plaintext.Match)
	require.JSONEq(t, testSpans, string(plaintext.Spans))
	require.False(t, plaintext.MatchEncrypted.Valid)
	require.False(t, plaintext.SpansEncrypted.Valid)

	provisionRiskEvidence(t, fixture, orgID)

	sealed, err := cipher.Seal(ctx, orgID, match, []byte(testSpans))
	require.NoError(t, err)
	require.False(t, sealed.Match.Valid)
	require.Nil(t, sealed.Spans)
	require.True(t, encryption.IsEnvelope(sealed.MatchEncrypted.String))
	require.True(t, encryption.IsEnvelope(sealed.SpansEncrypted.String))

	opened, err := cipher.OpenMatch(ctx, orgID, sealed.Match, sealed.MatchEncrypted)
	require.NoError(t, err)
	require.Equal(t, match, opened)

	spans, err := cipher.OpenSpans(ctx, orgID, sealed.Spans, sealed.SpansEncrypted)
	require.NoError(t, err)
	require.JSONEq(t, testSpans, string(spans))
}

func TestResultEvidenceCipher_WithoutOrganizationEncryptionKeepsPlaintext(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	match := pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true}

	for _, cipher := range []*risk.ResultEvidenceCipher{nil, risk.NewResultEvidenceCipher(nil)} {
		evidence, err := cipher.Seal(ctx, "org-without-encryption", match, nil)
		require.NoError(t, err)
		require.Equal(t, match, evidence.Match)
		require.False(t, evidence.MatchEncrypted.Valid)

		_, err = cipher.OpenMatch(ctx, "org-without-encryption", pgtype.Text{String: "", Valid: false}, pgtype.Text{String: encryption.EnvelopePrefix + "sealed", Valid: true})
		require.ErrorIs(t, err, risk.ErrResultEvidenceUnavailable)
	}
}

func TestResultEvidenceCipher_SealFailsClosedWhileSuspended(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	fixture := orgencryptiontest.New(t, ti.conn)
	cipher := risk.NewResultEvidenceCipher(fixture.Service)
	provisionRiskEvidence(t, fixture, orgID)

	require.NoError(t, fixture.Provisioner.Suspend(ctx, orgID, "customer disabled the key", orgencryption.SystemActor()))

	evidence, err := cipher.Seal(ctx, orgID, pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true}, []byte(testSpans))
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.Equal(t, risk.ResultEvidence{}, evidence, "a suspended organization's evidence is dropped, never stored in plaintext")
}

func TestResultEvidenceCipher_SealedMatchOpensOnlyForItsOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	other := seedOtherOrganization(t, ti)
	fixture := orgencryptiontest.New(t, ti.conn)
	cipher := risk.NewResultEvidenceCipher(fixture.Service)
	provisionRiskEvidence(t, fixture, orgID)
	provisionRiskEvidence(t, fixture, other.orgID)

	sealed, err := cipher.Seal(ctx, orgID, pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true}, nil)
	require.NoError(t, err)

	_, err = cipher.OpenMatch(ctx, other.orgID, sealed.Match, sealed.MatchEncrypted)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
}

func TestBackfillResultEvidence_SealsPlaintextRowsAndIsRepeatable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID
	fixture := orgencryptiontest.New(t, ti.conn)
	cipher := risk.NewResultEvidenceCipher(fixture.Service)

	policy := seedEvidencePolicy(t, ti, projectID, orgID)
	withSpans := insertEvidenceRow(t, ti, projectID, orgID, policy, pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true}, []byte(testSpans))
	matchOnly := insertEvidenceRow(t, ti, projectID, orgID, policy, pgtype.Text{String: "ghp_0123456789abcdefghijklmnopqrstuvwxyz", Valid: true}, nil)
	clean := insertEvidenceRow(t, ti, projectID, orgID, policy, pgtype.Text{String: "", Valid: false}, nil)

	other := seedOtherOrganization(t, ti)
	otherPolicy := seedEvidencePolicy(t, ti, other.projectID, other.orgID)
	otherRow := insertEvidenceRow(t, ti, other.projectID, other.orgID, otherPolicy, pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true}, nil)

	_, err := risk.BackfillResultEvidence(ctx, testenv.NewLogger(t), ti.conn, cipher, orgID, 1)
	require.ErrorIs(t, err, orgencryption.ErrNotProvisioned, "the backfill never runs before the organization has its key")

	provisionRiskEvidence(t, fixture, orgID)

	report, err := risk.BackfillResultEvidence(ctx, testenv.NewLogger(t), ti.conn, cipher, orgID, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), report.Sealed)
	require.Equal(t, int64(0), report.Skipped)

	rows := evidenceRowsByID(t, ti, projectID)
	for _, id := range []uuid.UUID{withSpans, matchOnly} {
		require.False(t, rows[id].Match.Valid, "plaintext match is cleared")
		require.Nil(t, rows[id].Spans, "plaintext spans are cleared")
		require.True(t, rows[id].MatchEncrypted.Valid)
	}
	require.True(t, rows[withSpans].SpansEncrypted.Valid)
	require.False(t, rows[matchOnly].SpansEncrypted.Valid)
	require.Equal(t, riskrepo.ListRiskResultEvidenceForTestRow{ID: clean, Source: "gitleaks", Match: pgtype.Text{}, Spans: nil, MatchEncrypted: pgtype.Text{}, SpansEncrypted: pgtype.Text{}, ExcludedExclusionID: uuid.NullUUID{}}, rows[clean])

	match, err := cipher.OpenMatch(ctx, orgID, rows[withSpans].Match, rows[withSpans].MatchEncrypted)
	require.NoError(t, err)
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", match.String)
	spans, err := cipher.OpenSpans(ctx, orgID, rows[withSpans].Spans, rows[withSpans].SpansEncrypted)
	require.NoError(t, err)
	require.JSONEq(t, testSpans, string(spans))

	otherRows := evidenceRowsByID(t, ti, other.projectID)
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", otherRows[otherRow].Match.String, "another organization's rows are untouched")
	require.False(t, otherRows[otherRow].MatchEncrypted.Valid)

	rerun, err := risk.BackfillResultEvidence(ctx, testenv.NewLogger(t), ti.conn, cipher, orgID, 1)
	require.NoError(t, err)
	require.Equal(t, int64(0), rerun.Sealed)

	remaining, err := risk.CountPlaintextResultEvidence(ctx, ti.conn, orgID)
	require.NoError(t, err)
	require.Equal(t, int64(0), remaining)
}

type evidenceTenant struct {
	orgID     string
	projectID uuid.UUID
}

func seedOtherOrganization(t *testing.T, ti *testInstance) evidenceTenant {
	t.Helper()
	ctx := t.Context()

	orgID := "other-org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgID,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "other-project",
		Slug:           "other-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return evidenceTenant{orgID: orgID, projectID: project.ID}
}

func seedEvidencePolicy(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string) riskrepo.RiskPolicy {
	t.Helper()

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		Name:           "evidence policy " + policyID.String()[:8],
		Sources:        []string{"gitleaks"},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
	})
	require.NoError(t, err)

	return policy
}

// insertEvidenceRow records a gitleaks result with the given plaintext
// evidence; an invalid match records a clean (found = FALSE) result.
func insertEvidenceRow(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string, policy riskrepo.RiskPolicy, match pgtype.Text, spans []byte) uuid.UUID {
	t.Helper()

	_, msgID := seedChatMessage(t, ti, projectID, orgID)
	id, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = riskrepo.New(ti.conn).InsertRiskResults(t.Context(), []riskrepo.InsertRiskResultsParams{{
		ID:                id,
		ProjectID:         projectID,
		OrganizationID:    orgID,
		RiskPolicyID:      policy.ID,
		RiskPolicyVersion: policy.Version,
		ChatMessageID:     uuid.NullUUID{UUID: msgID, Valid: true},
		Source:            "gitleaks",
		Found:             match.Valid,
		RuleID:            pgtype.Text{String: "aws-access-key-id", Valid: match.Valid},
		Match:             match,
		Spans:             spans,
	}})
	require.NoError(t, err)

	return id
}

func evidenceRowsByID(t *testing.T, ti *testInstance, projectID uuid.UUID) map[uuid.UUID]riskrepo.ListRiskResultEvidenceForTestRow {
	t.Helper()

	rows, err := riskrepo.New(ti.conn).ListRiskResultEvidenceForTest(t.Context(), projectID)
	require.NoError(t, err)
	byID := make(map[uuid.UUID]riskrepo.ListRiskResultEvidenceForTestRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	return byID
}
