package remotesessions_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type validationFixture struct {
	ti        *testInstance
	mgr       *remotesessions.ChallengeManager
	projectID uuid.UUID
	session   repo.RemoteSession
	ref       remotesessions.RemoteSessionRef
}

func seedValidationFixture(t *testing.T, prefix string) (context.Context, validationFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	issuerID := createRemoteIssuer(t, ctx, ti, prefix+"-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, prefix+"-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), prefix+"-client")
	subject := urn.NewUserSubject(prefix + "-subject")
	session := insertRemoteSession(t, ctx, ti.conn, subject, userIssuerID.String(), clientID)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	serverURL, err := url.Parse(testServerURL)
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, testenv.NewEncryptionClient(t), policy, ti.redisCache, serverURL)

	return ctx, validationFixture{
		ti:        ti,
		mgr:       mgr,
		projectID: *authCtx.ProjectID,
		session:   session,
		ref: remotesessions.RemoteSessionRef{
			ID:             session.ID,
			Subject:        subject,
			ClientID:       session.RemoteSessionClientID,
			UpdatedAt:      session.UpdatedAt.Time,
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
		},
	}
}

func (fx validationFixture) reload(t *testing.T, ctx context.Context) repo.RemoteSession {
	t.Helper()
	row, err := repo.New(fx.ti.conn).GetRemoteSessionByIDIncludingDeleted(ctx, repo.GetRemoteSessionByIDIncludingDeletedParams{
		ID:        fx.session.ID,
		ProjectID: conv.ToNullUUID(fx.projectID),
	})
	require.NoError(t, err)
	return row
}

func (fx validationFixture) reloadActive(t *testing.T, ctx context.Context) repo.RemoteSession {
	t.Helper()
	row, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.ref.Subject,
		RemoteSessionClientID: fx.ref.ClientID,
	})
	require.NoError(t, err)
	return row
}

func (fx validationFixture) recordValidation(t *testing.T, ctx context.Context, ref remotesessions.RemoteSessionRef, status remotesessions.ValidationOutcome, reason string, at time.Time) bool {
	t.Helper()
	written, err := fx.mgr.RecordRemoteSessionValidation(ctx, ref, remotesessions.RemoteSessionValidation{Status: status, Reason: reason, At: at})
	require.NoError(t, err)
	return written
}

func TestRecordRemoteSessionValidation_SetsVerdict(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-set")
	require.False(t, fx.session.LastValidatedAt.Valid, "a fresh grant starts never validated")
	require.False(t, fx.session.ValidationStatus.Valid)

	at := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	written := fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeRejectedByMember, "Rejected by linear", at)
	require.True(t, written)

	row := fx.reload(t, ctx)
	require.True(t, row.LastValidatedAt.Valid)
	require.WithinDuration(t, at, row.LastValidatedAt.Time, time.Millisecond)
	require.Equal(t, "rejected_by_member", row.ValidationStatus.String)
	require.Equal(t, "Rejected by linear", row.ValidationReason.String)
	// The CAS token and keepalive clock are not an observation of the credential, so they stay put.
	require.Equal(t, fx.session.UpdatedAt.Time, row.UpdatedAt.Time)

	// A valid verdict carries no reason; the column is NULL, not "".
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", time.Now())
	require.True(t, written)
	row = fx.reload(t, ctx)
	require.Equal(t, "valid", row.ValidationStatus.String)
	require.False(t, row.ValidationReason.Valid)
	require.Equal(t, fx.session.UpdatedAt.Time, row.UpdatedAt.Time, "a second verdict on the same grant still leaves the CAS token alone")
}

// The write is keyed on the whole (id, subject, client, updated_at) tuple.
func TestRecordRemoteSessionValidation_ScopedToSubjectAndClient(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-scope")
	at := time.Now()
	wrongSubject, wrongClient, wrongID, stale := fx.ref, fx.ref, fx.ref, fx.ref
	wrongSubject.Subject = urn.NewUserSubject("aim204-scope-other")
	wrongClient.ClientID = uuid.New()
	wrongID.ID = uuid.New()
	stale.UpdatedAt = fx.session.UpdatedAt.Time.Add(-time.Second)
	for name, ref := range map[string]remotesessions.RemoteSessionRef{
		"wrong subject": wrongSubject,
		"wrong client":  wrongClient,
		"wrong id":      wrongID,
		"stale CAS":     stale,
	} {
		require.False(t, fx.recordValidation(t, ctx, ref, remotesessions.ValidationOutcomeValid, "", at), name)
	}

	row := fx.reload(t, ctx)
	require.False(t, row.ValidationStatus.Valid, "no crafted key may reach the row")
}

// A project-owned client only accepts the project that owns it.
func TestRecordRemoteSessionValidation_ScopedToProject(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-tenant")
	at := time.Now()
	otherProject := fx.ref
	otherProject.ProjectID = uuid.New()
	require.False(t, fx.recordValidation(t, ctx, otherProject, remotesessions.ValidationOutcomeValid, "", at))
	require.False(t, fx.reload(t, ctx).ValidationStatus.Valid)

	require.True(t, fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", at), "the owning project still writes")
	require.Equal(t, "valid", fx.reload(t, ctx).ValidationStatus.String)
}

// An organization-owned client accepts any project reference from its organization,
// but rejects a reference carrying another organization.
func TestRecordRemoteSessionValidation_ScopedToOrganization(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-org-tenant")
	issuerID := seedGlobalRemoteIssuer(t, ctx, fx.ti.conn, "aim204-org-tenant-issuer")
	clientID := seedOrgLevelRemoteClient(t, ctx, fx.ti.conn, fx.ref.OrganizationID, issuerID, "aim204-org-tenant-client", fx.session.UserSessionIssuerID)
	fx.session = insertRemoteSession(t, ctx, fx.ti.conn, fx.ref.Subject, fx.session.UserSessionIssuerID.String(), clientID.String())
	fx.ref.ID = fx.session.ID
	fx.ref.ClientID = clientID
	fx.ref.UpdatedAt = fx.session.UpdatedAt.Time

	at := time.Now()
	otherOrganization := fx.ref
	otherOrganization.OrganizationID = "org_" + uuid.NewString()
	require.False(t, fx.recordValidation(t, ctx, otherOrganization, remotesessions.ValidationOutcomeValid, "", at))
	require.False(t, fx.reloadActive(t, ctx).ValidationStatus.Valid)

	otherProject := fx.ref
	otherProject.ProjectID = uuid.New()
	require.True(t, fx.recordValidation(t, ctx, otherProject, remotesessions.ValidationOutcomeValid, "", at), "another project in the owning organization still writes")
	require.Equal(t, "valid", fx.reloadActive(t, ctx).ValidationStatus.String)
}

// Platform clients are shared across projects and organizations, just as in the
// consent status query. The subject/client/CAS tuple still pins the exact grant.
func TestRecordRemoteSessionValidation_PlatformClient(t *testing.T) {
	t.Parallel()
	ctx, fx := seedValidationFixture(t, "validate-platform")
	issuerID := seedGlobalRemoteIssuer(t, ctx, fx.ti.conn, "validate-platform-issuer")
	client, err := repo.New(fx.ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "validate-platform-client",
	})
	require.NoError(t, err)
	fx.session = insertRemoteSession(t, ctx, fx.ti.conn, fx.ref.Subject, fx.session.UserSessionIssuerID.String(), client.ID.String())
	fx.ref.ID = fx.session.ID
	fx.ref.ClientID = client.ID
	fx.ref.UpdatedAt = fx.session.UpdatedAt.Time

	require.True(t, fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", time.Now()))
	row, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.ref.Subject,
		RemoteSessionClientID: fx.ref.ClientID,
	})
	require.NoError(t, err)
	require.Equal(t, "valid", row.ValidationStatus.String)

	wrongSubject := fx.ref
	wrongSubject.Subject = urn.NewUserSubject("validate-platform-other")
	require.False(t, fx.recordValidation(t, ctx, wrongSubject, remotesessions.ValidationOutcomeRejectedByMember, "Rejected by test member", time.Now()))
}

// Only the three probe verdicts are storable; anything else is refused before the write.
func TestRecordRemoteSessionValidation_RejectsNonProbeStatus(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-closed")
	for _, status := range []remotesessions.ValidationOutcome{"", remotesessions.ValidationOutcomeRevoked, "expired", "VALID"} {
		written, err := fx.mgr.RecordRemoteSessionValidation(ctx, fx.ref, remotesessions.RemoteSessionValidation{
			Status: status,
			Reason: "",
			At:     time.Now(),
		})
		require.Error(t, err, "status %q", status)
		require.ErrorContains(t, err, "is not a probe verdict")
		require.False(t, written)
	}
	row := fx.reload(t, ctx)
	require.False(t, row.ValidationStatus.Valid)
	require.False(t, row.LastValidatedAt.Valid)
}

// The never-downgrade rule holds in the row itself, so a late unknown from an overlapping probe cannot overwrite a valid.
func TestRecordRemoteSessionValidation_UnknownNeverOverwritesValid(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-race")
	written := fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", time.Now().Add(-time.Second))
	require.True(t, written)
	valid := fx.reload(t, ctx)

	// Same ref, same CAS token: the unknown reaches the row after the valid and loses.
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeUnknown, "linear did not answer in time", time.Now())
	require.False(t, written)
	row := fx.reload(t, ctx)
	require.Equal(t, "valid", row.ValidationStatus.String)
	require.False(t, row.ValidationReason.Valid)
	require.Equal(t, valid.LastValidatedAt.Time, row.LastValidatedAt.Time)

	// A rejection is decisive and still lands.
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeRejectedByMember, "Rejected by linear", time.Now())
	require.True(t, written)
	require.Equal(t, "rejected_by_member", fx.reload(t, ctx).ValidationStatus.String)

	// And an unknown over a rejection is recorded.
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeUnknown, "linear did not answer in time", time.Now())
	require.True(t, written)
	require.Equal(t, "unknown", fx.reload(t, ctx).ValidationStatus.String)
}

// Conclusive overlapping probes are ordered by when they started, not when they finish.
func TestRecordRemoteSessionValidation_OlderConclusiveVerdictCannotOverwriteNewer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-order")
	newerAt := time.Now()
	written := fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", newerAt)
	require.True(t, written)
	newer := fx.reload(t, ctx)

	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeRejectedByMember, "Rejected by upstream", newerAt.Add(-time.Second))
	require.False(t, written)

	row := fx.reload(t, ctx)
	require.Equal(t, "valid", row.ValidationStatus.String)
	require.False(t, row.ValidationReason.Valid)
	require.Equal(t, newer.LastValidatedAt.Time, row.LastValidatedAt.Time)
	require.Equal(t, fx.session.UpdatedAt.Time, row.UpdatedAt.Time, "validation ordering leaves the grant CAS token unchanged")
}

// Tombstones and re-established grants forget the last verdict.
func TestRemoteSessionValidation_ClearedByDeleteAndNewGrant(t *testing.T) {
	t.Parallel()

	ctx, fx := seedValidationFixture(t, "aim204-clear")
	written := fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", time.Now())
	require.True(t, written)

	// A new grant on the same (subject, client) replaces the row in place and resets the verdict.
	insertRemoteSession(t, ctx, fx.ti.conn, fx.ref.Subject, fx.session.UserSessionIssuerID.String(), fx.ref.ClientID.String())
	row := fx.reload(t, ctx)
	require.False(t, row.Deleted)
	require.False(t, row.LastValidatedAt.Valid)
	require.False(t, row.ValidationStatus.Valid)
	require.True(t, row.UpdatedAt.Time.After(fx.session.UpdatedAt.Time))

	// The verdict read before the reconnect no longer lands.
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeRejectedByMember, "Rejected by linear", time.Now())
	require.False(t, written, "a reconnect mid-probe leaves the new grant unvalidated")
	require.False(t, fx.reload(t, ctx).ValidationStatus.Valid)

	fx.ref.UpdatedAt = row.UpdatedAt.Time
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeRejectedByMember, "Rejected by linear", time.Now())
	require.True(t, written)

	tombstoned, err := repo.New(fx.ti.conn).SoftDeleteRemoteSessionsByClientID(ctx, fx.ref.ClientID)
	require.NoError(t, err)
	require.Len(t, tombstoned, 1)
	row = fx.reload(t, ctx)
	require.True(t, row.Deleted)
	require.False(t, row.LastValidatedAt.Valid)
	require.False(t, row.ValidationStatus.Valid)
	require.False(t, row.ValidationReason.Valid)

	// A tombstone is out of reach of the writer.
	written = fx.recordValidation(t, ctx, fx.ref, remotesessions.ValidationOutcomeValid, "", time.Now())
	require.False(t, written)
}

// A refreshed token has not been presented anywhere, so the refresh forgets the verdict.
func TestRemoteSessionValidation_ClearedByRefresh(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "aim204-refresh", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	before := getRemoteSessionRow(t, ctx, ti, clientID, subject)

	written, err := mgr.RecordRemoteSessionValidation(ctx, remotesessions.RemoteSessionRef{
		ID:             before.ID,
		Subject:        subject,
		ClientID:       clientID,
		UpdatedAt:      before.UpdatedAt.Time,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}, remotesessions.RemoteSessionValidation{
		Status: remotesessions.ValidationOutcomeValid,
		Reason: "",
		At:     time.Now(),
	})
	require.NoError(t, err)
	require.True(t, written)
	require.Equal(t, "valid", getRemoteSessionRow(t, ctx, ti, clientID, subject).ValidationStatus.String)

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, "")
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	after := getRemoteSessionRow(t, ctx, ti, clientID, subject)
	require.True(t, after.UpdatedAt.Time.After(before.UpdatedAt.Time), "the refresh rotates the CAS token")
	require.False(t, after.LastValidatedAt.Valid)
	require.False(t, after.ValidationStatus.Valid)
	require.False(t, after.ValidationReason.Valid)
}
