package hooks

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func feedbackPayload(skill, outcome string, note *string) *gen.SkillFeedbackPayload {
	return &gen.SkillFeedbackPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SchemaVersion:    hookSkillFeedbackSchemaV1,
		Skill:            skill,
		Outcome:          outcome,
		Note:             note,
	}
}

func TestSkillFeedback_RecordsDevFeedback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, ti.service.SkillFeedback(ctx, feedbackPayload("plugin-skill", "partially_helped", conv.PtrEmpty("missed an edge case"))))

	rows, err := skillsrepo.New(ti.conn).ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: *authCtx.ProjectID,
		SkillName: "plugin-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "dev", rows[0].Source)
	require.Equal(t, "partially_helped", rows[0].Outcome)
	require.Equal(t, "missed an edge case", rows[0].Note.String)
}

func TestSkillFeedback_EntitlementDisabledAcksWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: false, metadataOnly: false, fail: ""}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, ti.service.SkillFeedback(ctx, feedbackPayload("gated-skill", "helped", nil)))

	rows, err := skillsrepo.New(ti.conn).ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: *authCtx.ProjectID,
		SkillName: "gated-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestSkillFeedback_EntitlementLookupFailureErrors(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: productfeatures.FeatureSkills}

	err := ti.service.SkillFeedback(ctx, feedbackPayload("any-skill", "helped", nil))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestSkillFeedback_RejectsBadSchemaVersionAndOutcome(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}

	badVersion := feedbackPayload("plugin-skill", "helped", nil)
	badVersion.SchemaVersion = "hook.skill-feedback.v0"
	err := ti.service.SkillFeedback(ctx, badVersion)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)

	err = ti.service.SkillFeedback(ctx, feedbackPayload("plugin-skill", "changed_my_life", nil))
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestSkillFeedback_HTTPRouteRequiresAuthentication(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = captureFeatureStub{skills: true, metadataOnly: false, fail: ""}
	mux := goahttp.NewMuxer()
	Attach(mux, ti.service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	body := `{"schema_version":"hook.skill-feedback.v1","skill":"http-skill","outcome":"helped"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/rpc/hooks.skillFeedback", bytes.NewBufferString(body))
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
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/rpc/hooks.skillFeedback", bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "test-key")
	req.Header.Set("Gram-Project", "test-project")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	rows, err := skillsrepo.New(ti.conn).ListRecentSkillFeedback(ctx, skillsrepo.ListRecentSkillFeedbackParams{
		ProjectID: *authCtx.ProjectID,
		SkillName: "http-skill",
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}
