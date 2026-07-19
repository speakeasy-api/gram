package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	goasecurity "goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

type captureFeatureStub struct {
	skills       bool
	metadataOnly bool
	fail         productfeatures.Feature
}

type fixedHookAuthorizer struct{ authCtx *contextvalues.AuthContext }

func (a fixedHookAuthorizer) Authorize(ctx context.Context, _ string, _ *goasecurity.APIKeyScheme) (context.Context, error) {
	return contextvalues.SetAuthContext(ctx, a.authCtx), nil
}

func (s captureFeatureStub) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	if feature == s.fail {
		return false, errors.New("feature lookup failed")
	}
	switch feature {
	case productfeatures.FeatureSkills:
		return s.skills, nil
	case productfeatures.FeatureSkillCaptureMetadataOnly:
		return s.metadataOnly, nil
	default:
		return false, nil
	}
}

func captureManifest(name, body string) string {
	return "---\nname: " + name + "\ndescription: captured\n---\n\n" + body + "\n"
}

func rawHash(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func skillPayload(adapter, eventType, sessionID, name, hash string) *gen.IngestPayload {
	payload := canonicalIngestPayload(adapter, eventType, sessionID)
	payload.Data = &gen.HookIngestData{Skill: &gen.HookSkillData{Name: name, RawSha256: &hash}}
	return payload
}

func uploadPayload(content string) *gen.UploadSkillContentPayload {
	return &gen.UploadSkillContentPayload{
		ApikeyToken: nil, ProjectSlugInput: nil, SchemaVersion: hookSkillContentSchemaV1,
		RawSha256: rawHash(content), Content: content,
	}
}

func requireEffectMap(t *testing.T, effects map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := effects[key].(map[string]any)
	require.True(t, ok)
	return value
}

func TestIngest_RecordsExplicitAndInferredSkillObservations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	content := captureManifest("repo-review", "explicit")
	hash := rawHash(content)
	level, path, hostname := "project", "/workspace/.agents/skills/repo-review/SKILL.md", "devbox"
	explicit := skillPayload("claude", eventTypeSkillActivated, "explicit-session", "repo-review", strings.ToUpper(hash))
	explicit.IdempotencyKey = new(uuid.NewString())
	explicit.Source.Hostname = &hostname
	explicit.Data.Skill.SourceLevel = &level
	explicit.Data.Skill.SourcePath = &path
	result, err := ti.service.Ingest(ctx, explicit)
	require.NoError(t, err)
	captureEffect, ok := result.Effects["skill_capture"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, hash, captureEffect["raw_sha256"])
	require.Equal(t, true, captureEffect["content_required"])

	inferred := skillPayload("codex", "tool.requested", "inferred-session", "another-skill", "malformed")
	result, err = ti.service.Ingest(ctx, inferred)
	require.NoError(t, err)
	require.NotContains(t, result.Effects, "skill_capture")

	rows, err := ti.service.repo.ListSkillObservations(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "repo-review", rows[0].SkillName)
	require.Equal(t, hash, rows[0].RawSha256.String)
	require.Equal(t, level, rows[0].SourceLevel.String)
	require.Equal(t, path, rows[0].SourcePath.String)
	require.Equal(t, hostname, rows[0].Hostname.String)
	require.Equal(t, "another-skill", rows[1].SkillName)
	require.False(t, rows[1].RawSha256.Valid, "malformed hashes degrade to name-only observations")
}

func TestIngest_SkillObservationDurableIdempotencyIgnoresRedisDuplicate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	key := uuid.NewString()
	payload := skillPayload("claude", eventTypeSkillActivated, "duplicate-session", "repo-review", "")
	payload.IdempotencyKey = &key
	_, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	_, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)

	rows, err := ti.service.repo.ListSkillObservations(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, key, rows[0].IdempotencyKey.String)
}

func TestIngest_SkillObservationFailureDoesNotChangeVerdict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	failedCtx, cancel := context.WithCancel(ctx)
	cancel()

	result, err := ti.service.Ingest(failedCtx, skillPayload("claude", eventTypeSkillActivated, "failed-observation", "repo-review", strings.Repeat("a", 64)))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	require.NotContains(t, result.Effects, "skill_capture")
}

func TestIngest_BlockedInferredSkillIsNotObserved(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	toolName, identity := "mcp__local_server__search", "local-server"
	payload := skillPayload("codex", "tool.requested", "blocked-observation", "repo-review", "")
	payload.Data.ToolCall = &gen.HookToolCallData{Name: &toolName, Input: map[string]any{"query": "x"}}
	payload.Data.Mcp = &gen.HookMCPData{ServerIdentity: &identity}
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)

	rows, err := ti.service.repo.ListSkillObservations(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestSkillCapture_UnknownUploadThenKnown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	content := captureManifest("repo-review", "same content")
	hash := rawHash(content)

	first, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "unknown", "repo-review", hash))
	require.NoError(t, err)
	require.Equal(t, true, requireEffectMap(t, first.Effects, "skill_capture")["content_required"])
	require.NoError(t, ti.service.UploadSkillContent(ctx, uploadPayload(content)))

	second, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "known", "repo-review", hash))
	require.NoError(t, err)
	require.Equal(t, false, requireEffectMap(t, second.Effects, "skill_capture")["content_required"])
}

func TestIngest_ManualVersionRawHashIsKnownAndAliased(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	content := captureManifest("manual-known", "manual")
	hash := rawHash(content)
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: *authCtx.ProjectID, Name: "manual-known", DisplayName: "manual-known", Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	version, err := queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: content, CanonicalSha256: strings.Repeat("c", 64), RawSha256: hash,
		Description: pgtype.Text{}, Metadata: []byte(`{}`), SpecValid: true,
		ValidationErrors: []byte(`[]`), CreatedByUserID: authCtx.UserID,
		ProjectID: *authCtx.ProjectID, SkillID: skill.ID,
	})
	require.NoError(t, err)

	result, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "manual-known", "manual-known", hash))
	require.NoError(t, err)
	require.Equal(t, false, requireEffectMap(t, result.Effects, "skill_capture")["content_required"])
	alias, err := queries.GetSkillRawHash(ctx, skillsrepo.GetSkillRawHashParams{ProjectID: *authCtx.ProjectID, RawSha256: hash})
	require.NoError(t, err)
	require.Equal(t, version.CanonicalSha256, alias.CanonicalSha256)
}

func TestIngest_KnownSkillHashIsProjectLocal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherProject, err := projectrepo.New(ti.conn).CreateProject(ctx, projectrepo.CreateProjectParams{
		Name: "other-skill-project", Slug: "other-skill-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	content := captureManifest("project-local", "manual")
	hash := rawHash(content)
	queries := skillsrepo.New(ti.conn)
	skill, err := queries.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: otherProject.ID, Name: "project-local", DisplayName: "project-local", Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = queries.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: content, CanonicalSha256: strings.Repeat("d", 64), RawSha256: hash,
		Description: pgtype.Text{}, Metadata: []byte(`{}`), SpecValid: true,
		ValidationErrors: []byte(`[]`), CreatedByUserID: authCtx.UserID,
		ProjectID: otherProject.ID, SkillID: skill.ID,
	})
	require.NoError(t, err)

	result, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "project-local", "project-local", hash))
	require.NoError(t, err)
	require.Equal(t, true, requireEffectMap(t, result.Effects, "skill_capture")["content_required"])
	_, err = queries.GetSkillRawHash(ctx, skillsrepo.GetSkillRawHashParams{ProjectID: *authCtx.ProjectID, RawSha256: hash})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestIngest_PrivacyEntitlementAndLookupFailureOmitCaptureHint(t *testing.T) {
	t.Parallel()
	content := captureManifest("private-skill", "body")
	hash := rawHash(content)

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: false, metadataOnly: false, fail: ""}
	result, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "disabled", "private-skill", hash))
	require.NoError(t, err)
	require.NotContains(t, result.Effects, "skill_capture")

	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: true, fail: ""}
	result, err = ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "metadata-only", "private-skill", hash))
	require.NoError(t, err)
	require.NotContains(t, result.Effects, "skill_capture")
	require.Equal(t, true, requireEffectMap(t, result.Effects, "org_settings")["skill_capture_metadata_only"])

	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: productfeatures.FeatureSkillCaptureMetadataOnly}
	result, err = ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "lookup-failure", "private-skill", hash))
	require.NoError(t, err)
	require.NotContains(t, result.Effects, "skill_capture")
}

func TestUploadSkillContent_PrivacyAndEntitlementNoOpBeforeValidation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	invalid := &gen.UploadSkillContentPayload{ApikeyToken: nil, ProjectSlugInput: nil, SchemaVersion: "bad", RawSha256: "bad", Content: "bad"}

	ti.service.productFeatures = captureFeatureStub{skills: false, metadataOnly: false, fail: ""}
	require.NoError(t, ti.service.UploadSkillContent(ctx, invalid))
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: true, fail: ""}
	require.NoError(t, ti.service.UploadSkillContent(ctx, invalid))

	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: productfeatures.FeatureSkillCaptureMetadataOnly}
	err := ti.service.UploadSkillContent(ctx, invalid)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestUploadSkillContent_RejectsUnobservedHashWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	content := captureManifest("unsolicited-skill", "body")

	err := ti.service.UploadSkillContent(ctx, uploadPayload(content))
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = skillsrepo.New(ti.conn).GetSkillByNameForUpdate(ctx, skillsrepo.GetSkillByNameForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		Name:      "unsolicited-skill",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestUploadSkillContent_RejectsMalformedAndWrongHash(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}

	malformed := &gen.UploadSkillContentPayload{ApikeyToken: nil, ProjectSlugInput: nil, SchemaVersion: hookSkillContentSchemaV1, RawSha256: strings.Repeat("A", 64), Content: "x"}
	require.Error(t, ti.service.UploadSkillContent(ctx, malformed))
	wrong := &gen.UploadSkillContentPayload{ApikeyToken: nil, ProjectSlugInput: nil, SchemaVersion: hookSkillContentSchemaV1, RawSha256: strings.Repeat("0", 64), Content: "x"}
	require.Error(t, ti.service.UploadSkillContent(ctx, wrong))
}

func TestUploadSkillContent_RejectsMultibyteContentOverByteLimitWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	content := captureManifest("multibyte-oversized", strings.Repeat("界", 22_000))
	require.Less(t, utf8.RuneCountInString(content), 65_536)
	require.Greater(t, len(content), 65_536)
	_, err := ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "multibyte-oversized", "multibyte-oversized", rawHash(content)))
	require.NoError(t, err)
	err = ti.service.UploadSkillContent(ctx, uploadPayload(content))
	require.Error(t, err)
	require.ErrorContains(t, err, "skill manifest exceeds the 65536 byte limit")

	queries := skillsrepo.New(ti.conn)
	_, err = queries.GetSkillByNameForUpdate(ctx, skillsrepo.GetSkillByNameForUpdateParams{ProjectID: *authCtx.ProjectID, Name: "multibyte-oversized"})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = queries.GetSkillRawHash(ctx, skillsrepo.GetSkillRawHashParams{ProjectID: *authCtx.ProjectID, RawSha256: rawHash(content)})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestUploadSkillContent_HTTPRouteRequiresAuthentication(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	mux := goahttp.NewMuxer()
	Attach(mux, ti.service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	body := `{"schema_version":"hook.skill-content.v1","raw_sha256":"` + strings.Repeat("0", 64) + `","content":"x"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/rpc/hooks.uploadSkillContent", bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.auth = fixedHookAuthorizer{authCtx: authCtx}
	content := captureManifest("http-upload", "body")
	_, err = ti.service.Ingest(ctx, skillPayload("claude", eventTypeSkillActivated, "http-upload", "http-upload", rawHash(content)))
	require.NoError(t, err)
	encodedContent, err := json.Marshal(content)
	require.NoError(t, err)
	body = `{"schema_version":"hook.skill-content.v1","raw_sha256":"` + rawHash(content) + `","content":` + string(encodedContent) + `}`
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/rpc/hooks.uploadSkillContent", bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "test-key")
	req.Header.Set("Gram-Project", "test-project")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}
