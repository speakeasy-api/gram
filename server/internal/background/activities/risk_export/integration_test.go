package risk_export_test

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	risk_export "github.com/speakeasy-api/gram/server/internal/background/activities/risk_export"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func cloneDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "risk_export_testdb")
	require.NoError(t, err)
	return conn
}

type seeded struct {
	orgID      string
	projectID  uuid.UUID
	chatID     uuid.UUID
	messageIDs []uuid.UUID
}

// seedChatWithFinding creates an org/project/policy/chat with msgCount messages
// and an active finding on the message at findingIdx (0-based).
func seedChatWithFinding(t *testing.T, conn *pgxpool.Pool, msgCount, findingIdx int) seeded {
	t.Helper()
	ctx := t.Context()

	orgID := "test-org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: orgID, Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "test-project", Slug: "test-" + uuid.NewString()[:8], OrganizationID: orgID,
	})
	require.NoError(t, err)

	policyID := uuid.Must(uuid.NewV7())
	policy, err := riskrepo.New(conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: policyID, ProjectID: project.ID, OrganizationID: orgID, Name: "secrets",
		Sources: []string{"gitleaks"}, Enabled: true, Action: "flag", AudienceType: "everyone",
		AutoName: false, UserMessage: pgtype.Text{},
	})
	require.NoError(t, err)

	chatID := uuid.Must(uuid.NewV7())
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID: chatID, ProjectID: project.ID, OrganizationID: orgID,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
		Title: pgtype.Text{String: "test chat", Valid: true},
	})
	require.NoError(t, err)

	tq := testrepo.New(conn)
	ids := make([]uuid.UUID, 0, msgCount)
	for i := range msgCount {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgID, err := tq.InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true},
			Role:      role,
			Content:   "message body " + uuid.NewString()[:4],
		})
		require.NoError(t, err)
		ids = append(ids, msgID)
	}

	_, err = riskrepo.New(conn).InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID: uuid.New(), ProjectID: project.ID, OrganizationID: orgID,
		RiskPolicyID: policyID, RiskPolicyVersion: policy.Version, ChatMessageID: ids[findingIdx],
		Source: "gitleaks", Found: true,
		RuleID:      pgtype.Text{String: "secret.aws_key", Valid: true},
		Description: pgtype.Text{String: "leaked key", Valid: true},
		Match:       pgtype.Text{String: "AKIAEXAMPLE", Valid: true},
		StartPos:    pgtype.Int4{Int32: 0, Valid: true},
		EndPos:      pgtype.Int4{Int32: 11, Valid: true},
		Confidence:  pgtype.Float8{Float64: 1, Valid: true},
		Tags:        []string{"secret"}, Spans: nil, DeadLetterReason: pgtype.Text{},
	}})
	require.NoError(t, err)

	return seeded{orgID: orgID, projectID: project.ID, chatID: chatID, messageIDs: ids}
}

// outputDir returns the directory the export writes to. By default it is an
// ephemeral t.TempDir() (auto-cleaned, used in CI). Set RISK_EXPORT_TEST_OUTPUT
// to a persistent path (e.g. server/.assets/risk-export-demo) to keep the part
// files + manifest around for manual inspection after the run.
func outputDir(t *testing.T) string {
	t.Helper()
	if base := os.Getenv("RISK_EXPORT_TEST_OUTPUT"); base != "" {
		dir := filepath.Join(base, t.Name())
		require.NoError(t, os.MkdirAll(dir, 0o750))
		t.Logf("risk export output dir: %s", dir)
		return dir
	}
	return t.TempDir()
}

func newExport(t *testing.T, conn *pgxpool.Pool, localDir string) *risk_export.RiskExport {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	store := assets.NewFSBlobStore(testenv.NewLogger(t), root)
	return risk_export.NewRiskExport(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, store, localDir)
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var recs []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		recs = append(recs, m)
	}
	require.NoError(t, sc.Err())
	return recs
}

func TestExport_FindingCentric_EndToEnd(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	localDir := outputDir(t)
	sd := seedChatWithFinding(t, conn, 6, 3) // finding on the 4th message (rn=4)
	ex := newExport(t, conn, localDir)

	filters := risk_export.Filters{OrganizationID: sd.orgID, HasFindingsOnly: true}
	sampling := risk_export.Sampling{Percent: 100, Seed: 1}

	count, err := ex.CountExportRows(ctx, risk_export.CountExportRowsArgs{Filters: filters, Sampling: sampling})
	require.NoError(t, err)
	require.EqualValues(t, 1, count.ChatCount)

	page, err := ex.FetchExportChatPage(ctx, risk_export.FetchExportChatPageArgs{
		Filters: filters, Sampling: sampling, AfterID: nil, PageSize: 100,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{sd.chatID}, page.ChatIDs)
	require.False(t, page.HasMore)

	res, err := ex.WriteExportChunk(ctx, risk_export.WriteExportChunkArgs{
		Mode: risk_export.ModeFindingCentric, Filters: filters, ContextSize: 1,
		ChatIDs: page.ChatIDs, OutputPrefix: "risk-exports/test/" + sd.orgID, PartIndex: 0,
		TargetKind: "local",
	})
	require.NoError(t, err)

	recs := readJSONL(t, filepath.Join(localDir, res.ObjectPath))
	// finding on rn=4 with context_size=1 => rn 3,4,5 (3 windowed messages).
	require.Len(t, recs, 3)

	var seedRows, findingRows int
	for _, r := range recs {
		if seed, ok := r["is_seed"].(bool); ok && seed {
			seedRows++
		}
		if f, ok := r["finding"].(map[string]any); ok {
			findingRows++
			require.Equal(t, "secret.aws_key", f["rule_id"])
			require.Equal(t, "AKIAEXAMPLE", f["match"])
		}
	}
	require.Equal(t, 1, seedRows)
	require.Equal(t, 1, findingRows)
}

func TestExport_FullTranscript_EndToEnd(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	localDir := outputDir(t)
	sd := seedChatWithFinding(t, conn, 6, 3)
	ex := newExport(t, conn, localDir)

	filters := risk_export.Filters{OrganizationID: sd.orgID}

	res, err := ex.WriteExportChunk(ctx, risk_export.WriteExportChunkArgs{
		Mode: risk_export.ModeFullTranscript, Filters: filters,
		ChatIDs: []uuid.UUID{sd.chatID}, OutputPrefix: "risk-exports/test/" + sd.orgID, PartIndex: 0,
		TargetKind: "local",
	})
	require.NoError(t, err)

	recs := readJSONL(t, filepath.Join(localDir, res.ObjectPath))
	// All 6 messages present; exactly one carries the finding.
	require.Len(t, recs, 6)
	findingRows := 0
	for _, r := range recs {
		if _, ok := r["finding"].(map[string]any); ok {
			findingRows++
		}
	}
	require.Equal(t, 1, findingRows)

	// Finalize writes a manifest with the run totals.
	fin, err := ex.FinalizeExport(ctx, risk_export.FinalizeExportArgs{
		OutputPrefix: "risk-exports/test/" + sd.orgID, TargetKind: "local",
		SignedTTL: 0, Manifest: risk_export.Manifest{
			RequestID: uuid.NewString(), Mode: risk_export.ModeFullTranscript,
			OrganizationID: sd.orgID, TotalChats: 1, TotalRows: res.RowCount,
			Parts: []string{res.ObjectPath}, SchemaVersion: risk_export.ManifestSchemaVersion,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, fin.ManifestObjectPath)

	raw, err := os.ReadFile(filepath.Join(localDir, fin.ManifestObjectPath))
	require.NoError(t, err)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.EqualValues(t, 6, manifest["total_rows"])
	require.EqualValues(t, risk_export.ManifestSchemaVersion, manifest["schema_version"])
}

func TestExport_SamplingExcludesChats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	sd := seedChatWithFinding(t, conn, 3, 0)

	// 0% sample keeps nothing.
	ex := newExport(t, conn, t.TempDir())
	count, err := ex.CountExportRows(ctx, risk_export.CountExportRowsArgs{
		Filters:  risk_export.Filters{OrganizationID: sd.orgID},
		Sampling: risk_export.Sampling{Percent: 0, Seed: 1},
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, count.ChatCount)
}
