package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type sweepFixture struct {
	pool      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
	chatID    uuid.UUID
}

func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()

	pool, err := infra.CloneTestDatabase(t, "sweep_builtin_presets")
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(pool).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "sweep test org",
		Slug:               "sweep-" + uuid.NewString()[:8],
		GramAccountType:    "free",
		Whitelisted:        true,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: now, Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: now.AddDate(0, 0, 14), Valid: true},
	}))
	project, err := projectsrepo.New(pool).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "sweep",
		Slug:           "sweep",
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	chatID, err := chatrepo.New(pool).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             uuid.Must(uuid.NewV7()),
		ProjectID:      project.ID,
		OrganizationID: orgID,
		UserID:         conv.ToPGText(""),
		ExternalUserID: conv.ToPGText(""),
	})
	require.NoError(t, err)
	return &sweepFixture{pool: pool, orgID: orgID, projectID: project.ID, chatID: chatID}
}

func (f *sweepFixture) newPolicy(t *testing.T, id uuid.UUID, enabled bool, analyzerConfig []byte, disabledRules []string) uuid.UUID {
	t.Helper()
	policy, err := riskrepo.New(f.pool).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:                   id,
		ProjectID:            f.projectID,
		OrganizationID:       f.orgID,
		Name:                 "sweep policy",
		PolicyType:           "standard",
		Sources:              []string{"presidio", "custom"},
		PresidioEntities:     []string{},
		AnalyzerConfig:       analyzerConfig,
		PromptInjectionRules: []string{},
		DisabledRules:        disabledRules,
		CustomRuleIds:        []string{},
		McpScope:             nil,
		Enabled:              enabled,
		Action:               "flag",
		AudienceType:         "everyone",
		ShadowMcpDisposition: pgtype.Text{},
		AutoName:             true,
		UserMessage:          pgtype.Text{},
		Prompt:               pgtype.Text{},
		ModelConfig:          nil,
		Score:                pgtype.Float8{},
	})
	require.NoError(t, err)
	return policy.ID
}

func (f *sweepFixture) newFinding(t *testing.T, policyID uuid.UUID, createdAt time.Time, source, ruleID, match string) chrepo.RiskFindingRow {
	t.Helper()
	messageID, err := testrepo.New(f.pool).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    f.chatID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Role:      "user",
		Content:   match,
	})
	require.NoError(t, err)
	return chrepo.RiskFindingRow{
		ID:                uuid.Must(uuid.NewV7()),
		CreatedAt:         createdAt,
		OrganizationID:    f.orgID,
		ProjectID:         f.projectID.String(),
		ChatMessageID:     messageID.String(),
		RiskPolicyID:      policyID.String(),
		RiskPolicyVersion: 1,
		RuleID:            ruleID,
		Source:            source,
		Confidence:        1,
		Tags:              []string{},
		StartPos:          0,
		EndPos:            int32(len(match)),
		ChatID:            f.chatID.String(),
		MessageCreatedAt:  createdAt,
		MatchLen:          uint32(len(match)),
		MatchRedacted:     maskdisplay.Display(source, ruleID, match),
		Surface:           "content",
		Field:             "content",
		EventKind:         chrepo.EventKindFinding,
	}
}

func (f *sweepFixture) newContentPartFinding(
	t *testing.T,
	assetStorage assets.BlobStore,
	policyID uuid.UUID,
	createdAt time.Time,
	source string,
	ruleID string,
	match string,
) chrepo.RiskFindingRow {
	t.Helper()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), f.pool, assetStorage)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	assetURL, err := writer.WriteContentPartAsset(t.Context(), f.projectID, f.chatID, []byte(match))
	require.NoError(t, err)
	partID, err := riskrepo.New(f.pool).CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              f.chatID,
		ProjectID:           uuid.NullUUID{UUID: f.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     assetURL,
		ParentChatMessageID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	finding := f.newFinding(t, policyID, createdAt, source, ruleID, match)
	finding.ChatMessageID = ""
	finding.ContentPartID = partID.String()
	return finding
}

func runCHSweep(t *testing.T, fixture *sweepFixture, from, to time.Time, dryRun bool, batchSize int, assetStorage blobio.Reader) report {
	t.Helper()
	lib, err := presetlib.New()
	require.NoError(t, err)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	rep, err := sweep(t.Context(), fixture.pool, chrepo.New(conn), config{
		orgID:      fixture.orgID,
		projectID:  fixture.projectID,
		from:       from,
		to:         to,
		batchSize:  batchSize,
		dryRun:     dryRun,
		candidates: candidateSourceClickHouse,
	}, lib, assetStorage)
	require.NoError(t, err)
	return rep
}

func rawCopyCount(t *testing.T, id uuid.UUID) uint64 {
	t.Helper()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var count uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count() FROM risk_findings WHERE id = ?`, id).Scan(&count))
	return count
}

func findingState(t *testing.T, fixture *sweepFixture, id uuid.UUID) chrepo.FalsePositiveState {
	t.Helper()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	states, err := chrepo.New(conn).ListFalsePositiveStates(t.Context(), fixture.orgID, fixture.projectID.String(), []uuid.UUID{id})
	require.NoError(t, err)
	require.Len(t, states, 1)
	return states[0]
}

func TestSweepClickHouseDisabledRulesSkipsDismissedAndShadowAndIncludesUnsuppression(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, []string{"custom.disabled"})
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	live := fixture.newFinding(t, policyID, createdAt, "custom", "custom.disabled", "live")
	dismissed := fixture.newFinding(t, policyID, createdAt, "custom", "custom.disabled", "dismissed")
	shadow := fixture.newFinding(t, policyID, createdAt, "custom", "custom.disabled", "shadow")
	shadow.Shadow = true
	restored := fixture.newFinding(t, policyID, createdAt, "custom", "custom.disabled", "restored")

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := chrepo.New(conn)
	require.NoError(t, queries.InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{live, dismissed, shadow, restored}))
	now := chrepo.FormatCHTime(time.Now().UTC())
	require.NoError(t, queries.AppendFalsePositiveSuppression(t.Context(), fixture.orgID, fixture.projectID.String(), now, now, "manual", []uuid.UUID{dismissed.ID, restored.ID}))
	require.NoError(t, queries.AppendFalsePositiveReversal(t.Context(), fixture.orgID, fixture.projectID.String(), chrepo.FormatCHTime(time.Now().UTC().Add(time.Second)), []uuid.UUID{restored.ID}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 1, nil)
	require.EqualValues(t, 2, rep.scanned)
	require.EqualValues(t, 2, rep.flagged)
	require.EqualValues(t, 2, rep.updated)
	require.EqualValues(t, 2, rep.byFilter[filterDisabledRule])
	require.True(t, findingState(t, fixture, live.ID).Dismissed)
	require.True(t, findingState(t, fixture, restored.ID).Dismissed)
	require.EqualValues(t, 2, rawCopyCount(t, dismissed.ID))
	require.EqualValues(t, 1, rawCopyCount(t, shadow.ID))

	var detail string
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT excluded_detail
		FROM risk_findings
		WHERE organization_id = ? AND project_id = ? AND id = ?
		ORDER BY (event_kind IN ('suppression', 'unsuppression')) DESC, inserted_at DESC
		LIMIT 1
	`, fixture.orgID, fixture.projectID.String(), live.ID).Scan(&detail))
	require.Equal(t, "disabled_rule:custom.disabled", detail)
}

func TestSweepClickHouseHonorsBuiltinPresetToggleAndReportsUnavailablePolicies(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	enabledPolicy := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, nil)
	presetDisabledPolicy := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, []byte(`{"builtin_presets":{"enabled":false}}`), nil)
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	const card = "4111111111111111"
	enabled := fixture.newFinding(t, enabledPolicy, createdAt, "presidio", "pii.credit_card", card)
	presetDisabled := fixture.newFinding(t, presetDisabledPolicy, createdAt, "presidio", "pii.credit_card", card)
	missing := fixture.newFinding(t, uuid.New(), createdAt, "presidio", "pii.credit_card", card)
	malformed := fixture.newFinding(t, uuid.New(), createdAt, "presidio", "pii.credit_card", card)
	malformed.RiskPolicyID = "not-a-uuid"

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{enabled, presetDisabled, missing, malformed}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.EqualValues(t, 4, rep.scanned)
	require.EqualValues(t, 1, rep.flagged)
	require.EqualValues(t, 1, rep.updated)
	require.EqualValues(t, 2, rep.missingPolicy)
	require.Zero(t, rep.disabledPolicy)
	require.EqualValues(t, 1, rep.byFilter[filterBuiltinPreset])
	require.True(t, findingState(t, fixture, enabled.ID).Dismissed)
	require.False(t, findingState(t, fixture, presetDisabled.ID).Dismissed)
	require.False(t, findingState(t, fixture, missing.ID).Dismissed)
	require.False(t, findingState(t, fixture, malformed.ID).Dismissed)
}

func TestSweepClickHouseDryRunWritesNothingAndRerunChangesNothing(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, []string{"custom.disabled"})
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	finding := fixture.newFinding(t, policyID, createdAt, "custom", "custom.disabled", "candidate")
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))

	dry := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), true, 10, nil)
	require.EqualValues(t, 1, dry.flagged)
	require.Zero(t, dry.updated)
	require.EqualValues(t, 1, rawCopyCount(t, finding.ID))
	require.False(t, findingState(t, fixture, finding.ID).Dismissed)

	applied := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.EqualValues(t, 1, applied.updated)
	require.EqualValues(t, 2, rawCopyCount(t, finding.ID))

	rerun := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.Zero(t, rerun.flagged)
	require.Zero(t, rerun.updated)
	require.EqualValues(t, 2, rawCopyCount(t, finding.ID))
}

func TestSweepClickHouseLeavesDisabledPolicyFindingsLive(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), false, nil, []string{"custom.disabled"})
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	finding := fixture.newFinding(t, policyID, createdAt, "custom", "custom.disabled", "candidate")
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.EqualValues(t, 1, rep.scanned)
	require.EqualValues(t, 1, rep.disabledPolicy)
	require.Zero(t, rep.missingPolicy)
	require.Zero(t, rep.flagged)
	require.False(t, findingState(t, fixture, finding.ID).Dismissed)
}

func TestSweepClickHouseRejectsRedactionMismatch(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, nil)
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	const card = "4111111111111111"
	finding := fixture.newFinding(t, policyID, createdAt, "presidio", "pii.credit_card", card)
	finding.MatchRedacted = maskdisplay.Display("presidio", "pii.credit_card", "4242424242424242")
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.EqualValues(t, 1, rep.scanned)
	require.EqualValues(t, 1, rep.redactionMismatch)
	require.Zero(t, rep.flagged)
	require.False(t, findingState(t, fixture, finding.ID).Dismissed)
}

func TestSweepClickHouseMatchesCatalogRuleWithoutRuleIDScope(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, nil)
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	const exampleSecret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	finding := fixture.newFinding(t, policyID, createdAt, "gitleaks", "secret.generic-api-key", exampleSecret)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.EqualValues(t, 1, rep.flagged)
	require.EqualValues(t, 1, rep.byFilter[filterBuiltinPreset])
	require.True(t, findingState(t, fixture, finding.ID).Dismissed)
}

func TestLoadPolicySweepFiltersRetriesMissingPolicyOnNextPage(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := uuid.Must(uuid.NewV7())
	cache := make(map[uuid.UUID]policySweepFilters)
	firstPageMissing := make(map[uuid.UUID]struct{})

	filters, err := loadPolicySweepFilters(t.Context(), fixture.pool, cache, firstPageMissing, fixture.projectID, policyID.String())
	require.NoError(t, err)
	require.False(t, filters.found)
	require.False(t, filters.enabled)
	require.Contains(t, firstPageMissing, policyID)

	fixture.newPolicy(t, policyID, true, nil, nil)
	filters, err = loadPolicySweepFilters(t.Context(), fixture.pool, cache, firstPageMissing, fixture.projectID, policyID.String())
	require.NoError(t, err)
	require.False(t, filters.found)
	require.False(t, filters.enabled)

	filters, err = loadPolicySweepFilters(t.Context(), fixture.pool, cache, make(map[uuid.UUID]struct{}), fixture.projectID, policyID.String())
	require.NoError(t, err)
	require.True(t, filters.found)
	require.True(t, filters.enabled)
}

func TestSweepClickHouseReconstructsContentPartWithAssetStorage(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, nil)
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	const card = "4111111111111111"
	assetStorage := assetstest.NewTestBlobStore(t)
	finding := fixture.newContentPartFinding(t, assetStorage, policyID, createdAt, "presidio", "pii.credit_card", card)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, assetStorage)
	require.Zero(t, rep.unreconstructable)
	require.EqualValues(t, 1, rep.flagged)
	require.True(t, findingState(t, fixture, finding.ID).Dismissed)
}

func TestSweepClickHouseReportsContentPartWithoutAssetStorage(t *testing.T) {
	t.Parallel()
	fixture := newSweepFixture(t)
	policyID := fixture.newPolicy(t, uuid.Must(uuid.NewV7()), true, nil, nil)
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	const card = "4111111111111111"
	assetStorage := assetstest.NewTestBlobStore(t)
	finding := fixture.newContentPartFinding(t, assetStorage, policyID, createdAt, "presidio", "pii.credit_card", card)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.NoError(t, chrepo.New(conn).InsertRiskFindings(t.Context(), []chrepo.RiskFindingRow{finding}))

	rep := runCHSweep(t, fixture, createdAt.Add(-time.Hour), createdAt.Add(time.Hour), false, 10, nil)
	require.EqualValues(t, 1, rep.unreconstructable)
	require.Zero(t, rep.flagged)
	require.False(t, findingState(t, fixture, finding.ID).Dismissed)
}
