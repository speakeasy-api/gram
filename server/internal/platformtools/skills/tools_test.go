package skills

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type stubSkillsService struct {
	listPayload              *genskills.ListPayload
	getPayload               *genskills.GetPayload
	listVersionsPayload      *genskills.ListVersionsPayload
	listDistributionsPayload *genskills.ListDistributionsPayload
}

func (s *stubSkillsService) List(_ context.Context, payload *genskills.ListPayload) (*genskills.ListSkillsResult, error) {
	s.listPayload = payload
	return &genskills.ListSkillsResult{Skills: nil, NextCursor: nil}, nil
}

func (s *stubSkillsService) Get(_ context.Context, payload *genskills.GetPayload) (*genskills.GetSkillResult, error) {
	s.getPayload = payload
	return &genskills.GetSkillResult{Skill: nil, LatestVersion: nil}, nil
}

func (s *stubSkillsService) ListVersions(_ context.Context, payload *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error) {
	s.listVersionsPayload = payload
	return &genskills.ListSkillVersionsResult{Versions: nil, NextCursor: nil}, nil
}

func (s *stubSkillsService) ListDistributions(_ context.Context, payload *genskills.ListDistributionsPayload) (*genskills.ListSkillDistributionsResult, error) {
	s.listDistributionsPayload = payload
	return &genskills.ListSkillDistributionsResult{Distributions: nil, NextCursor: nil}, nil
}

func TestToolsForwardReadFiltersWithoutAuthOverrides(t *testing.T) {
	t.Parallel()

	svc := &stubSkillsService{}
	env := toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}
	var out bytes.Buffer

	err := NewListTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"cursor":"next"}`), &out)
	require.NoError(t, err)
	require.Equal(t, 50, svc.listPayload.Limit)
	require.Equal(t, "next", *svc.listPayload.Cursor)
	require.Nil(t, svc.listPayload.SessionToken)
	require.Nil(t, svc.listPayload.ApikeyToken)
	require.Nil(t, svc.listPayload.ProjectSlugInput)

	out.Reset()
	err = NewGetTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"id":"skill-id"}`), &out)
	require.NoError(t, err)
	require.Equal(t, "skill-id", svc.getPayload.ID)
	require.Nil(t, svc.getPayload.SessionToken)
	require.Nil(t, svc.getPayload.ApikeyToken)
	require.Nil(t, svc.getPayload.ProjectSlugInput)

	out.Reset()
	err = NewListVersionsTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"id":"skill-id","limit":7}`), &out)
	require.NoError(t, err)
	require.Equal(t, "skill-id", svc.listVersionsPayload.ID)
	require.Equal(t, 7, svc.listVersionsPayload.Limit)
	require.Nil(t, svc.listVersionsPayload.SessionToken)
	require.Nil(t, svc.listVersionsPayload.ApikeyToken)
	require.Nil(t, svc.listVersionsPayload.ProjectSlugInput)

	out.Reset()
	err = NewListDistributionsTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"plugin_id":"plugin-id"}`), &out)
	require.NoError(t, err)
	require.Equal(t, 20, svc.listDistributionsPayload.Limit)
	require.Equal(t, "plugin-id", *svc.listDistributionsPayload.PluginID)
	require.Nil(t, svc.listDistributionsPayload.SessionToken)
	require.Nil(t, svc.listDistributionsPayload.ApikeyToken)
	require.Nil(t, svc.listDistributionsPayload.ProjectSlugInput)
}

func TestToolsRejectInvalidLimits(t *testing.T) {
	t.Parallel()

	svc := &stubSkillsService{}
	env := toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}
	var out bytes.Buffer

	err := NewListTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"limit":-1}`), &out)
	require.ErrorContains(t, err, "limit must be between 1 and 200")
	require.Nil(t, svc.listPayload)

	err = NewListVersionsTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"id":"skill-id","limit":-1}`), &out)
	require.ErrorContains(t, err, "limit must be between 1 and 50")
	require.Nil(t, svc.listVersionsPayload)

	err = NewListDistributionsTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"limit":-1}`), &out)
	require.ErrorContains(t, err, "limit must be between 1 and 50")
	require.Nil(t, svc.listDistributionsPayload)
}
