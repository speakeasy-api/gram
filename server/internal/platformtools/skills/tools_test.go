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
	createPayload            *genskills.CreatePayload
	listPayload              *genskills.ListPayload
	getPayload               *genskills.GetPayload
	listVersionsPayload      *genskills.ListVersionsPayload
	listDistributionsPayload *genskills.ListDistributionsPayload
	listResult               *genskills.ListSkillsResult
	getResult                *genskills.GetSkillResult
	getErr                   error
	listVersionsResult       *genskills.ListSkillVersionsResult
}

func (s *stubSkillsService) Create(_ context.Context, payload *genskills.CreatePayload) (*genskills.RecordSkillResult, error) {
	s.createPayload = payload
	return &genskills.RecordSkillResult{
		Skill:          nil,
		Version:        nil,
		CreatedSkill:   true,
		CreatedVersion: true,
	}, nil
}

func (s *stubSkillsService) List(_ context.Context, payload *genskills.ListPayload) (*genskills.ListSkillsResult, error) {
	s.listPayload = payload
	if s.listResult != nil {
		return s.listResult, nil
	}
	return &genskills.ListSkillsResult{Skills: nil, NextCursor: nil}, nil
}

func (s *stubSkillsService) Get(_ context.Context, payload *genskills.GetPayload) (*genskills.GetSkillResult, error) {
	s.getPayload = payload
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.getResult != nil {
		return s.getResult, nil
	}
	return &genskills.GetSkillResult{Skill: nil, LatestVersion: nil}, nil
}

func (s *stubSkillsService) ListVersions(_ context.Context, payload *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error) {
	s.listVersionsPayload = payload
	if s.listVersionsResult != nil {
		return s.listVersionsResult, nil
	}
	return &genskills.ListSkillVersionsResult{Versions: nil, NextCursor: nil}, nil
}

func (s *stubSkillsService) ListDistributions(_ context.Context, payload *genskills.ListDistributionsPayload) (*genskills.ListSkillDistributionsResult, error) {
	s.listDistributionsPayload = payload
	return &genskills.ListSkillDistributionsResult{Distributions: nil, NextCursor: nil}, nil
}

func TestToolsForwardPayloadsWithoutAuthOverrides(t *testing.T) {
	t.Parallel()

	svc := &stubSkillsService{}
	env := toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
	}
	var out bytes.Buffer

	err := NewCreateTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"content":"---\nname: test-skill\ndescription: Test.\n---\n\n# Test"}`), &out)
	require.NoError(t, err)
	require.Contains(t, svc.createPayload.Content, "name: test-skill")
	require.Nil(t, svc.createPayload.SessionToken)
	require.Nil(t, svc.createPayload.ApikeyToken)
	require.Nil(t, svc.createPayload.ProjectSlugInput)

	out.Reset()
	err = NewListTool(svc).Call(t.Context(), env, bytes.NewBufferString(`{"cursor":"next"}`), &out)
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

func TestCreateToolDescriptorMarksMutationAsIdempotent(t *testing.T) {
	t.Parallel()

	descriptor := NewCreateTool(nil).Descriptor()
	require.Equal(t, "platform_create_skill", descriptor.Name)
	require.NotNil(t, descriptor.Annotations)
	require.False(t, *descriptor.Annotations.ReadOnlyHint)
	require.False(t, *descriptor.Annotations.DestructiveHint)
	require.True(t, *descriptor.Annotations.IdempotentHint)
	require.False(t, *descriptor.Annotations.OpenWorldHint)
	require.JSONEq(t, `{
		"additionalProperties": false,
		"properties": {
			"content": {
				"description": "The complete SKILL.md content, including YAML frontmatter and instructions.",
				"type": "string"
			}
		},
		"required": ["content"],
		"type": "object"
	}`, string(descriptor.InputSchema))
}

func TestCreateToolRejectsMissingContent(t *testing.T) {
	t.Parallel()

	svc := &stubSkillsService{}
	var out bytes.Buffer
	err := NewCreateTool(svc).Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
	}, bytes.NewBufferString(`{}`), &out)
	require.ErrorContains(t, err, "content is required")
	require.Nil(t, svc.createPayload)
}

func TestToolsRejectInvalidLimits(t *testing.T) {
	t.Parallel()

	svc := &stubSkillsService{}
	env := toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
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
