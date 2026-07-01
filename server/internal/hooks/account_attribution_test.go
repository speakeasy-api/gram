package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

func TestClassifyAccountType_TeamWhenEmailResolves(t *testing.T) {
	t.Parallel()
	require.Equal(t, accountTypeTeam, classifyAccountType("user-123"))
}

func TestClassifyAccountType_PersonalWhenEmailUnresolved(t *testing.T) {
	t.Parallel()
	require.Equal(t, accountTypePersonal, classifyAccountType(""))
}

// TestLogs_AttributesTeamAndPersonalAccountsViaDeviceBridge drives the full OTEL
// ingest path twice on one device: a team session (work email resolves to an org
// member) followed by a personal session (gmail, same user.id). It asserts the
// team account is classified team and linked directly, the device bridge learns
// the owner, and the later personal account — whose email does not resolve — is
// classified personal yet attributed to the same employee through the bridge.
func TestLogs_AttributesTeamAndPersonalAccountsViaDeviceBridge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	queries := repo.New(ti.conn)

	userID := "employee-user-id"
	workEmail := "employee@example.com"
	seedHookUser(t, ctx, ti.conn, orgID, userID, workEmail)

	const (
		deviceID        = "device-shared-1"
		teamAccountUUID = "acct-team-uuid"
		teamOrgID       = "enterprise-org-id"
		persAccountUUID = "acct-personal-uuid"
		persOrgID       = "max-org-id"
	)
	now := time.Now().UTC().Truncate(time.Second)

	// Team session: work email resolves to the seeded org member.
	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(now)),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "team-session"),
				strAttr("user.email", workEmail),
				strAttr("organization.id", teamOrgID),
				strAttr("user.account_uuid", teamAccountUUID),
				strAttr("user.account_id", "user_team_tagged"),
				strAttr("user.id", deviceID),
			},
		},
	))
	require.NoError(t, err)

	teamAccount, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID:      orgID,
		Provider:            providerAnthropic,
		ExternalAccountUuid: teamAccountUUID,
	})
	require.NoError(t, err)
	require.Equal(t, accountTypeTeam, teamAccount.AccountType.String)
	require.Equal(t, userID, teamAccount.UserID.String)
	require.Equal(t, teamOrgID, teamAccount.ExternalOrgID.String)
	require.Equal(t, workEmail, teamAccount.Email.String)

	// The team session taught the device bridge who owns this machine.
	deviceOwner, err := queries.GetDeviceOwner(ctx, repo.GetDeviceOwnerParams{
		OrganizationID: orgID,
		Provider:       providerAnthropic,
		DeviceID:       deviceID,
	})
	require.NoError(t, err)
	require.Equal(t, userID, deviceOwner.LinkedUserID.String)

	// Personal session on the SAME device: a gmail that does not resolve.
	err = ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(now.Add(time.Minute))),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "personal-session"),
				strAttr("user.email", "someone@gmail.com"),
				strAttr("organization.id", persOrgID),
				strAttr("user.account_uuid", persAccountUUID),
				strAttr("user.account_id", "user_personal_tagged"),
				strAttr("user.id", deviceID),
			},
		},
	))
	require.NoError(t, err)

	personalAccount, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID:      orgID,
		Provider:            providerAnthropic,
		ExternalAccountUuid: persAccountUUID,
	})
	require.NoError(t, err)
	// Classified personal (email did not resolve) but attributed to the employee
	// learned from the team session on the same device.
	require.Equal(t, accountTypePersonal, personalAccount.AccountType.String)
	require.Equal(t, userID, personalAccount.UserID.String)
	require.Equal(t, persOrgID, personalAccount.ExternalOrgID.String)
}

// claudeAccountSession POSTs a single-record Claude OTEL logs payload carrying a
// full account identity, the shape the live OTEL stream emits.
func claudeAccountSession(t *testing.T, ctx context.Context, ti *testInstance, sessionID, email, externalOrgID, accountUUID, deviceID string, ts time.Time) {
	t.Helper()
	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(ts)),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", email),
				strAttr("organization.id", externalOrgID),
				strAttr("user.account_uuid", accountUUID),
				strAttr("user.id", deviceID),
			},
		},
	))
	require.NoError(t, err)
}

// TestLogs_DowngradesPersonalAccountOnWorkEmail covers the edge case where an
// employee runs a personal account signed in with their work email. Email
// resolution alone would call it team; the shared-org heuristic downgrades it to
// personal because the employee's enterprise org is shared by other employees
// while the personal org is solo.
func TestLogs_DowngradesPersonalAccountOnWorkEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	queries := repo.New(ti.conn)
	now := time.Now().UTC().Truncate(time.Second)

	const enterpriseOrg = "enterprise-org-shared"
	userA, emailA := "employee-a", "a@example.com"
	userB, emailB := "employee-b", "b@example.com"
	seedHookUser(t, ctx, ti.conn, orgID, userA, emailA)
	seedHookUser(t, ctx, ti.conn, orgID, userB, emailB)

	// Two employees under the same enterprise org -> it becomes "shared".
	claudeAccountSession(t, ctx, ti, "ent-a", emailA, enterpriseOrg, "acct-ent-a", "device-a", now)
	claudeAccountSession(t, ctx, ti, "ent-b", emailB, enterpriseOrg, "acct-ent-b", "device-b", now)

	// Employee A's enterprise account is team.
	entA, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: "acct-ent-a",
	})
	require.NoError(t, err)
	require.Equal(t, accountTypeTeam, entA.AccountType.String)

	// Employee A now runs a personal account on their WORK email: resolves to an
	// org member, but a solo provider org while they also use the shared
	// enterprise org -> downgraded to personal.
	claudeAccountSession(t, ctx, ti, "max-a", emailA, "max-org-solo", "acct-max-a", "device-max-a", now.Add(time.Minute))

	maxA, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: "acct-max-a",
	})
	require.NoError(t, err)
	require.Equal(t, accountTypePersonal, maxA.AccountType.String)
	require.Equal(t, userA, maxA.UserID.String)
}

// TestLogs_SingleAccountEnterpriseStaysTeam confirms the heuristic never
// downgrades a lone enterprise account: with no other shared org for the
// employee, email resolution wins and it stays team.
func TestLogs_SingleAccountEnterpriseStaysTeam(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	queries := repo.New(ti.conn)
	now := time.Now().UTC().Truncate(time.Second)

	userID, email := "solo-employee", "solo@example.com"
	seedHookUser(t, ctx, ti.conn, orgID, userID, email)

	claudeAccountSession(t, ctx, ti, "solo-session", email, "solo-enterprise-org", "acct-solo", "device-solo", now)

	acct, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: "acct-solo",
	})
	require.NoError(t, err)
	require.Equal(t, accountTypeTeam, acct.AccountType.String)
	require.Equal(t, userID, acct.UserID.String)
}

// TestLogs_AttributesOncePerSession confirms attribution runs only on a session's
// first batch: a later batch for the same session reuses the cached result and
// does not re-classify, even after the session's email becomes a connected user.
// (Reclassification happens on the next session, not mid-session.)
func TestLogs_AttributesOncePerSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	queries := repo.New(ti.conn)
	now := time.Now().UTC().Truncate(time.Second)

	email := "later-member@example.com"
	// First batch: email does not resolve yet -> classified personal.
	claudeAccountSession(t, ctx, ti, "repeat-session", email, "some-org", "acct-repeat", "device-repeat", now)

	first, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: "acct-repeat",
	})
	require.NoError(t, err)
	require.Equal(t, accountTypePersonal, first.AccountType.String)

	// The email now becomes a connected org member — which WOULD classify team.
	seedHookUser(t, ctx, ti.conn, orgID, "later-member", email)

	// Second batch for the SAME session reuses the cached attribution and does
	// not re-run classification: account_type stays personal.
	claudeAccountSession(t, ctx, ti, "repeat-session", email, "some-org", "acct-repeat", "device-repeat", now.Add(time.Minute))

	second, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: "acct-repeat",
	})
	require.NoError(t, err)
	require.Equal(t, accountTypePersonal, second.AccountType.String)
}

// TestLogs_EnrichesAttributionWhenIdentityArrivesAcrossBatches covers a session
// whose records a collector split across batches: the first batch carrying the
// account UUID lacks the work email (so it classifies personal), and a later
// batch is the first to carry the resolving email. The later batch must re-run
// attribution — reclassifying to team, persisting the email/org, and teaching the
// device bridge — rather than short-circuiting on the cached personal result.
func TestLogs_EnrichesAttributionWhenIdentityArrivesAcrossBatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	queries := repo.New(ti.conn)
	now := time.Now().UTC().Truncate(time.Second)

	userID, workEmail := "split-employee", "split@example.com"
	seedHookUser(t, ctx, ti.conn, orgID, userID, workEmail)

	const (
		sessionID   = "split-session"
		accountUUID = "acct-split"
		extOrgID    = "split-ent-org"
		deviceID    = "device-split"
	)

	// First batch: account UUID + device but NO work email -> classified personal.
	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(now)),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.account_uuid", accountUUID),
				strAttr("user.id", deviceID),
			},
		},
	))
	require.NoError(t, err)

	first, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: accountUUID,
	})
	require.NoError(t, err)
	require.Equal(t, accountTypePersonal, first.AccountType.String)
	require.False(t, first.UserID.Valid)

	// Second batch for the SAME session is the first to carry the work email (and
	// the provider org). Attribution must re-run and reclassify to team.
	err = ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(now.Add(time.Minute))),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", workEmail),
				strAttr("organization.id", extOrgID),
				strAttr("user.account_uuid", accountUUID),
				strAttr("user.id", deviceID),
			},
		},
	))
	require.NoError(t, err)

	enriched, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: accountUUID,
	})
	require.NoError(t, err)
	require.Equal(t, accountTypeTeam, enriched.AccountType.String)
	require.Equal(t, userID, enriched.UserID.String)
	require.Equal(t, workEmail, enriched.Email.String)
	require.Equal(t, extOrgID, enriched.ExternalOrgID.String)

	// The late-arriving team session also taught the device bridge.
	owner, err := queries.GetDeviceOwner(ctx, repo.GetDeviceOwnerParams{
		OrganizationID: orgID, Provider: providerAnthropic, DeviceID: deviceID,
	})
	require.NoError(t, err)
	require.Equal(t, userID, owner.LinkedUserID.String)
}

// TestLogs_StampsAccountAttributionOnTelemetry confirms the account attribution
// (provider, account_type, external_org_id) lands on the telemetry_logs rows so
// org-level usage dashboards can split by personal vs team account.
func TestLogs_StampsAccountAttributionOnTelemetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	email := "stamp-user@example.com"
	seedHookUser(t, ctx, ti.conn, orgID, "stamp-user", email)
	claudeAccountSession(t, ctx, ti, "stamp-session", email, "stamp-ent-org", "acct-stamp", "device-stamp", now)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, now, 1)
	require.Contains(t, logs[0].Attributes, providerAnthropic)
	require.Contains(t, logs[0].Attributes, accountTypeTeam)
	require.Contains(t, logs[0].Attributes, "stamp-ent-org")
}

// TestClaude_LinksChatToUserAccount confirms a captured chat is stamped with the
// user_accounts id attributed on the OTEL path, so chats can be filtered/grouped
// by the AI account that produced them.
func TestClaude_LinksChatToUserAccount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "hello from a personal-account session"
	userAccountID := uuid.NewString()

	// Seed session metadata as the OTEL path would for a personal account: a
	// non-resolving email, but the device bridge already attributed it to an
	// employee and the account entity is linked.
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     "personal@gmail.com",
		UserID:        "bridged-employee",
		Provider:      providerAnthropic,
		AccountType:   accountTypePersonal,
		UserAccountID: userAccountID,
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}, time.Hour))

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var chat chatRepo.GetChatRow
	require.Eventually(t, func() bool {
		var err error
		chat, err = chatRepo.New(ti.conn).GetChat(ctx, chatID)
		return err == nil
	}, 2*time.Second, 25*time.Millisecond)

	require.True(t, chat.UserAccountID.Valid)
	require.Equal(t, userAccountID, chat.UserAccountID.UUID.String())
	// The bridged owner is preserved through hook persistence (not discarded by
	// re-resolving the non-resolving personal email).
	require.Equal(t, "bridged-employee", chat.UserID.String)
}

// TestLogs_NoAccountIdentityDoesNotCreateUserAccount confirms attribution is a
// no-op for sessions that carry no provider account id (older clients): no
// user_accounts row is created and ingest still succeeds.
func TestLogs_NoAccountIdentityDoesNotCreateUserAccount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	now := time.Now().UTC().Truncate(time.Second)

	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(now)),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "identityless-session"),
				strAttr("user.email", "someone@example.com"),
			},
		},
	))
	require.NoError(t, err)

	_, err = repo.New(ti.conn).GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		Provider:            providerAnthropic,
		ExternalAccountUuid: "",
	})
	require.Error(t, err)
}

// TestLogs_LateBridgeBackfillsPersonalAccount covers the "late linking" ordering:
// an employee uses their personal account BEFORE any team session exists on the
// device, so it is first attributed with no owner; later a team session teaches
// the device bridge, and a subsequent personal session backfills the personal
// account's user_id to the employee. This is distinct from the team-first
// ordering in TestLogs_AttributesTeamAndPersonalAccountsViaDeviceBridge.
func TestLogs_LateBridgeBackfillsPersonalAccount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	queries := repo.New(ti.conn)
	now := time.Now().UTC().Truncate(time.Second)

	userID, workEmail := "late-employee", "late@example.com"
	seedHookUser(t, ctx, ti.conn, orgID, userID, workEmail)

	const (
		deviceID = "device-late-1"
		persAcct = "acct-late-personal"
		persOrg  = "late-max-org"
		teamAcct = "acct-late-team"
		teamOrg  = "late-enterprise-org"
	)

	// 1) Personal session first — gmail doesn't resolve and no bridge owner
	//    exists yet, so the personal account is created unattributed.
	claudeAccountSession(t, ctx, ti, "late-pers-1", "late-person@gmail.com", persOrg, persAcct, deviceID, now)

	pers1, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: persAcct,
	})
	require.NoError(t, err)
	require.Equal(t, accountTypePersonal, pers1.AccountType.String)
	require.Empty(t, pers1.UserID.String, "personal account starts unattributed (no bridge yet)")

	// 2) Team session on the SAME device — work email resolves, teaching the
	//    device -> employee bridge.
	claudeAccountSession(t, ctx, ti, "late-team-1", workEmail, teamOrg, teamAcct, deviceID, now.Add(time.Minute))

	owner, err := queries.GetDeviceOwner(ctx, repo.GetDeviceOwnerParams{
		OrganizationID: orgID, Provider: providerAnthropic, DeviceID: deviceID,
	})
	require.NoError(t, err)
	require.Equal(t, userID, owner.LinkedUserID.String, "team session teaches the bridge")

	// 3) A later personal session (new session id, same account + device) adopts
	//    the learned owner and backfills the personal account's user_id.
	claudeAccountSession(t, ctx, ti, "late-pers-2", "late-person@gmail.com", persOrg, persAcct, deviceID, now.Add(2*time.Minute))

	pers2, err := queries.GetUserAccount(ctx, repo.GetUserAccountParams{
		OrganizationID: orgID, Provider: providerAnthropic, ExternalAccountUuid: persAcct,
	})
	require.NoError(t, err)
	require.Equal(t, accountTypePersonal, pers2.AccountType.String, "stays personal")
	require.Equal(t, userID, pers2.UserID.String, "personal account backfilled to the employee via the bridge")

	// The personal session (empty email resolution) must not clobber the learned
	// device owner — COALESCE preserves it.
	ownerAfter, err := queries.GetDeviceOwner(ctx, repo.GetDeviceOwnerParams{
		OrganizationID: orgID, Provider: providerAnthropic, DeviceID: deviceID,
	})
	require.NoError(t, err)
	require.Equal(t, userID, ownerAfter.LinkedUserID.String, "personal session does not clobber the device owner")
}

// TestStampAccountAttribution_StampsAllNonEmptyFields verifies the telemetry
// stamp writes every account attribute when the session metadata carries them.
func TestStampAccountAttribution_StampsAllNonEmptyFields(t *testing.T) {
	t.Parallel()

	attrs := map[attr.Key]any{}
	stampAccountAttribution(attrs, SessionMetadata{
		Provider:      providerAnthropic,
		ExternalOrgID: "ext-org-1",
		AccountType:   accountTypePersonal,
		BillingMode:   "flat_rate",
		DeviceID:      "device-1",
	})

	require.Equal(t, providerAnthropic, attrs[attr.ProviderKey])
	require.Equal(t, "ext-org-1", attrs[attr.ExternalOrgIDKey])
	require.Equal(t, accountTypePersonal, attrs[attr.AccountTypeKey])
	require.Equal(t, "flat_rate", attrs[attr.BillingModeKey])
	require.Equal(t, "device-1", attrs[attr.DeviceIDKey])
}

// TestResolveBillingMode_AccountOverrideWins verifies the per-account override is
// authoritative and short-circuits the org-level lookup.
func TestResolveBillingMode_AccountOverrideWins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)

	meta := &SessionMetadata{
		GramOrgID:     authCtx.ActiveOrganizationID,
		Provider:      providerAnthropic,
		ExternalOrgID: "ent-org",
	}
	mode, err := ti.service.resolveBillingMode(ctx, meta, "metered")
	require.NoError(t, err)
	require.Equal(t, "metered", mode)
}

// TestResolveBillingMode_EmptyWhenNoDeclaration verifies that with no account
// override and no org-level declaration, resolution returns empty (treated as
// unknown upstream — never assume real cost). Exercises the ErrNoRows path of
// GetProviderOrgBillingMode against a real DB.
func TestResolveBillingMode_EmptyWhenNoDeclaration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)

	meta := &SessionMetadata{
		GramOrgID:     authCtx.ActiveOrganizationID,
		Provider:      providerAnthropic,
		ExternalOrgID: "ent-org",
	}
	mode, err := ti.service.resolveBillingMode(ctx, meta, "")
	require.NoError(t, err)
	require.Empty(t, mode)
}

// TestProviderBillingConfigProvider verifies the mapping from a session provider
// tag to the ai_integration_configs provider identifier where org-level billing
// modes are declared. Claude sessions tag 'anthropic' but the config lives under
// 'anthropic_compliance'; other providers map to themselves.
func TestProviderBillingConfigProvider(t *testing.T) {
	t.Parallel()

	require.Equal(t, "anthropic_compliance", providerBillingConfigProvider(providerAnthropic))
	require.Equal(t, providerCursor, providerBillingConfigProvider(providerCursor))
	require.Equal(t, providerOpenAI, providerBillingConfigProvider(providerOpenAI))
	require.Empty(t, providerBillingConfigProvider(""))
}

// TestStampAccountAttribution_SkipsEmptyFields verifies an unclassified or
// identity-less session stamps nothing (zero value) and that only the non-empty
// fields are written for a partial one — so the columns stay empty rather than
// getting blanks.
func TestStampAccountAttribution_SkipsEmptyFields(t *testing.T) {
	t.Parallel()

	// Zero-value metadata (a map miss for a session with no attribution).
	empty := map[attr.Key]any{}
	stampAccountAttribution(empty, SessionMetadata{})
	require.Empty(t, empty, "zero-value metadata stamps nothing")

	// Only the provider is set (e.g. Codex/Cursor, which tag provider but do not
	// classify) — nothing else should be written.
	partial := map[attr.Key]any{}
	stampAccountAttribution(partial, SessionMetadata{Provider: providerOpenAI})
	require.Equal(t, providerOpenAI, partial[attr.ProviderKey])
	require.NotContains(t, partial, attr.AccountTypeKey)
	require.NotContains(t, partial, attr.ExternalOrgIDKey)
	require.NotContains(t, partial, attr.BillingModeKey)
	require.NotContains(t, partial, attr.DeviceIDKey)
}
