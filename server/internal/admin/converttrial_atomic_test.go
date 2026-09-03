package admin

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestEnterpriseTrialConversionAuditActor_FallsBackWithoutSafeInternalID(t *testing.T) {
	t.Parallel()
	actor, display := enterpriseTrialConversionAuditActor(t.Context())
	require.Equal(t, "system", actor.ID)
	require.Nil(t, display)
}

func TestEnterpriseTrialConversionAuditActor_UsesOnlySafeHumanDisplayNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		authName    string
		email       string
		wantDisplay *string
	}{
		{name: "email fallback", email: "staff@example.test"},
		{name: "email with different casing and surrounding whitespace", authName: "  Staff@Example.Test  ", email: "staff@example.test"},
		{name: "arbitrary email-shaped name", authName: "alias@other.example", email: "staff@example.test"},
		{name: "valid human name is normalized", authName: "  Staff Operator  ", email: "staff@example.test", wantDisplay: new("Staff Operator")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
				OIDCSubject: "staff-subject",
				Name:        tt.authName,
				Email:       tt.email,
			})
			actor, display := enterpriseTrialConversionAuditActor(ctx)
			require.Equal(t, "staff-subject", actor.ID)
			require.Equal(t, tt.wantDisplay, display)
		})
	}
}

func TestConversionTrialAuditSnapshot_CanonicalStatus(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	valid := func(value time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: value, Valid: true}
	}
	tests := []struct {
		name        string
		endsAt      time.Time
		convertedAt pgtype.Timestamptz
		demotedAt   pgtype.Timestamptz
		want        string
	}{
		{name: "running", endsAt: now.Add(8 * 24 * time.Hour), want: "running"},
		{name: "ending soon", endsAt: now.Add(24 * time.Hour), want: "ending_soon"},
		{name: "expired undemoted", endsAt: now.Add(-time.Hour), want: "expired"},
		{name: "demoted takes precedence over dates", endsAt: now.Add(-time.Hour), demotedAt: valid(now.Add(-2 * time.Hour)), want: "demoted"},
		{name: "converted takes precedence over demotion", endsAt: now.Add(-time.Hour), convertedAt: valid(now), demotedAt: valid(now.Add(-2 * time.Hour)), want: "converted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snapshot := conversionTrialAuditSnapshot("enterprise", valid(tt.endsAt), tt.convertedAt, tt.demotedAt, now)
			require.Equal(t, tt.want, snapshot.Status)
		})
	}
}

func TestMarkEnterpriseTrialConverted_AuditSnapshotsAreCompleteAndPrivate(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	const (
		orgID            = "org_convert_audit"
		sessionSentinel  = "bearer-session-token-privacy-sentinel"
		emailSentinel    = "conversion-operator@privacy.invalid"
		operatorName     = "Conversion Operator"
		oidcSentinel     = "external-oidc-subject-privacy-sentinel"
		workosSentinel   = "external-workos-privacy-sentinel"
		nameSentinel     = "organization-name-privacy-sentinel"
		slugSentinel     = "organization-slug-privacy-sentinel"
		providerSentinel = "provider-payload-privacy-sentinel"
		promptSentinel   = "prompt-privacy-sentinel"
		spendSentinel    = "313131.313131"
	)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: sessionSentinel, Email: emailSentinel, OIDCSubject: oidcSentinel, Name: operatorName, HD: "privacy.invalid"})
	endsAt := time.Now().UTC().Add(7 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-time.Hour)
	workosID := workosSentinel
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: nameSentinel, slug: slugSentinel, accountType: "free", workosID: &workosID, whitelisted: false})
	projectID := seedProject(t, ctx, conn, orgID, "conversion-privacy")
	require.NoError(t, testrepo.New(conn).SeedPromptTemplatePrivacyFixture(ctx, testrepo.SeedPromptTemplatePrivacyFixtureParams{ProjectID: projectID, Prompt: promptSentinel}))
	require.NoError(t, testrepo.New(conn).SeedOpenRouterSpendPrivacyFixture(ctx, testrepo.SeedOpenRouterSpendPrivacyFixtureParams{OrganizationID: orgID, SpendUsd: spendSentinel}))
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}
	require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), Disabled: true, DisableCauses: []string{"trial_demotion", "billing_inactive", "admin_lock"}}))
	require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyProviderPayloadFixture(ctx, testrepo.SetOpenRouterAPIKeyProviderPayloadFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), ProviderPayload: conv.ToPGText(providerSentinel)}))

	payloadSessionToken := sessionSentinel
	result, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID, AdminSessionToken: &payloadSessionToken})
	require.NoError(t, err)
	require.Empty(t, provisioner.reconcileAttempts, "conversion must not use disabled-only reconciliation")
	require.Equal(t, openrouter.AllKeyTypes, provisioner.conversionPolicyAttempts)
	responseJSON, err := json.Marshal(srv.NewMarkEnterpriseTrialConvertedResponseBody(result))
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(responseJSON, &response))
	require.ElementsMatch(t, []string{"organization_id", "converted_at"}, mapKeys(response))
	require.Equal(t, orgID, response["organization_id"])
	require.NotEmpty(t, response["converted_at"])
	record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, oidcSentinel, record.ActorID)
	require.Equal(t, operatorName, record.ActorDisplay)
	require.Empty(t, record.ActorSlug)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(record.Metadata, &metadata))
	require.Equal(t, map[string]any{"conversion_source": "admin", "key_access_changed": true}, metadata)
	for _, raw := range [][]byte{record.BeforeSnapshot, record.AfterSnapshot} {
		var snapshot struct {
			Organization map[string]any   `json:"organization"`
			Trial        map[string]any   `json:"trial"`
			Keys         []map[string]any `json:"keys"`
		}
		require.NoError(t, json.Unmarshal(raw, &snapshot))
		require.ElementsMatch(t, []string{"account_type", "whitelisted", "disabled"}, mapKeys(snapshot.Organization))
		require.ElementsMatch(t, []string{"status", "tier", "ends_at", "converted_at", "demoted_at"}, mapKeys(snapshot.Trial))
		require.Len(t, snapshot.Keys, len(openrouter.AllKeyTypes))
		for _, key := range snapshot.Keys {
			require.ElementsMatch(t, []string{"key_type", "stored_disabled", "effective_disabled", "key_access_changed", "monthly_credits"}, mapKeys(key))
			require.NotContains(t, key, "disable_causes")
		}
	}

	var before, after struct {
		Trial map[string]any   `json:"trial"`
		Keys  []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(record.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(record.AfterSnapshot, &after))
	require.Equal(t, "demoted", before.Trial["status"])
	require.Equal(t, "converted", after.Trial["status"])
	chatBefore := keyAuditSnapshotByType(t, before.Keys, openrouter.KeyTypeChat)
	chatAfter := keyAuditSnapshotByType(t, after.Keys, openrouter.KeyTypeChat)
	internalBefore := keyAuditSnapshotByType(t, before.Keys, openrouter.KeyTypeInternal)
	internalAfter := keyAuditSnapshotByType(t, after.Keys, openrouter.KeyTypeInternal)
	require.Equal(t, true, chatBefore["stored_disabled"])
	require.Equal(t, true, chatBefore["effective_disabled"])
	require.Equal(t, false, chatBefore["key_access_changed"])
	require.Equal(t, true, chatAfter["stored_disabled"])
	require.Equal(t, true, chatAfter["effective_disabled"])
	require.Equal(t, false, chatAfter["key_access_changed"])
	require.Equal(t, true, internalBefore["stored_disabled"])
	require.Equal(t, true, internalBefore["effective_disabled"])
	require.Equal(t, true, internalBefore["key_access_changed"])
	require.Equal(t, false, internalAfter["stored_disabled"])
	require.Equal(t, false, internalAfter["effective_disabled"])
	require.Equal(t, true, internalAfter["key_access_changed"])
	floor, ok := openrouter.DefaultCreditLimit(orgID, "enterprise", false)
	require.True(t, ok)
	require.EqualValues(t, floor, chatAfter["monthly_credits"])
	require.EqualValues(t, floor, internalAfter["monthly_credits"])

	envelope, err := audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, audittestrepo.GetLatestOutboxPayloadByOrgParams{OrganizationID: orgID, EventType: string(events.OrganizationEnterpriseTrialV1.EventType())})
	require.NoError(t, err)
	var event webhooksv1.Event
	require.NoError(t, proto.Unmarshal(envelope, &event))
	var payload events.AuditLogCreatedPayloadV1
	require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
	require.Equal(t, record.ActorID, payload.ActorID)
	require.Equal(t, audit.SpeakeasyTeamActorLabel, payload.ActorDisplayName)
	require.Equal(t, string(audit.ActionOrganizationEnterpriseTrialConverted), payload.Action)
	require.NotEmpty(t, payload.BeforeSnapshot)
	require.NotEmpty(t, payload.AfterSnapshot)
	require.NotEmpty(t, payload.Metadata)

	fullAuditRow, err := json.Marshal(map[string]any{
		"action": record.Action, "organization_id": record.OrganizationID, "project_id": record.ProjectID,
		"actor_id": record.ActorID, "actor_type": record.ActorType, "actor_display_name": record.ActorDisplayName, "actor_slug": record.ActorSlug,
		"subject_id": record.SubjectID, "subject_type": record.SubjectType, "subject_display": record.SubjectDisplay, "subject_slug": record.SubjectSlug,
		"metadata": json.RawMessage(record.Metadata), "before_snapshot": json.RawMessage(record.BeforeSnapshot), "after_snapshot": json.RawMessage(record.AfterSnapshot),
		"acting_surface": record.ActingSurface, "acting_client_id": record.ActingClientID,
	})
	require.NoError(t, err)
	for surface, data := range map[string][]byte{"audit row": fullAuditRow, "outbox envelope": envelope, "endpoint response": responseJSON} {
		for _, forbidden := range []string{sessionSentinel, emailSentinel, workosSentinel, nameSentinel, slugSentinel, providerSentinel, promptSentinel, spendSentinel, "hash-", "sk-test"} {
			require.NotContains(t, string(data), forbidden, "%s leaked %s", surface, forbidden)
		}
	}
}

func keyAuditSnapshotByType(t *testing.T, keys []map[string]any, keyType openrouter.KeyType) map[string]any {
	t.Helper()
	for _, key := range keys {
		if key["key_type"] == string(keyType) {
			return key
		}
	}
	require.FailNow(t, "missing key audit snapshot", string(keyType))
	return nil
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return keys
}

func TestMarkEnterpriseTrialConverted_AuditFailureRollsBackEverything(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_audit_rollback"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7, disabled: true})
	beforeOrg, beforeTrial, beforeKey := readOrgState(t, ctx, conn, orgID), readTrial(t, ctx, conn, orgID), readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted))

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
	require.Equal(t, beforeTrial, readTrial(t, ctx, conn, orgID))
	require.Equal(t, beforeKey, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat))
	require.Empty(t, provisioner.reconcileAttempts)
}

func TestMarkEnterpriseTrialConverted_OutboxFailureRollsBackEverything(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_outbox_rollback"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7, disabled: true})
	beforeOrg, beforeTrial, beforeKey := readOrgState(t, ctx, conn, orgID), readTrial(t, ctx, conn, orgID), readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	err := testrepo.New(conn).RejectPublishOutboxWritesFixture(ctx)
	require.NoError(t, err)

	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Equal(t, beforeOrg, readOrgState(t, ctx, conn, orgID))
	require.Equal(t, beforeTrial, readTrial(t, ctx, conn, orgID))
	require.Equal(t, beforeKey, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat))
	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Empty(t, provisioner.reconcileAttempts)
}

func TestMarkEnterpriseTrialConverted_UnclassifiedKeyRollsBack(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_null_key"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: time.Now().UTC().Add(time.Hour)})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 7})
	require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), Disabled: false, DisableCauses: nil}))

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.False(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
	require.Empty(t, provisioner.reconcileAttempts)
}

func TestMarkEnterpriseTrialConverted_PostCommitFailureRetryConverges(t *testing.T) {
	t.Parallel()
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_retry"
	demotedAt := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: demotedAt, demotedAt: &demotedAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 7, disabled: true})
	}
	provisioner.failOn, provisioner.failAfter, provisioner.failWith = openrouter.KeyTypeInternal, 1, errors.New("provider unavailable")
	notifier := &fakeTrialNotifier{inactiveErr: errors.New("notification cleanup unavailable")}
	svc.trial = notifier

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.True(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid, "provider failure occurs after commit")
	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	require.Equal(t, []string{orgID}, notifier.inactive)
	provisioner.failWith = nil
	notifier.inactiveErr = nil
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	count, err = audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, []string{orgID, orgID}, notifier.inactive, "valid retries must reattempt transient TrialInactive cleanup")
	require.Empty(t, provisioner.reconcileAttempts, "conversion must not use disabled-only reconciliation")
	require.Equal(t, append(append([]openrouter.KeyType{}, openrouter.AllKeyTypes...), openrouter.AllKeyTypes...), provisioner.conversionPolicyAttempts, "valid retry must repeat complete conversion-policy reconciliation")
}
