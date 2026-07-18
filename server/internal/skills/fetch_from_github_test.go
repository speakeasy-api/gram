package skills_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/oops"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

func TestFetchFromGitHubParsesSkillsAndReportsIssues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	repositoryURL := "https://github.com/example/repository"
	ti.repositoryReader.On("FetchSkillFiles", mock.Anything, repositoryURL).Return(&ghclient.PublicRepositorySnapshot{
		URL:           repositoryURL,
		FullName:      "example/repository",
		DefaultBranch: "main",
		CommitSHA:     "abc123",
		Visibility:    "public",
		Files: []ghclient.PublicRepositoryFile{
			{Path: "skills/valid/SKILL.md", Content: skillManifest("valid-skill", "A valid skill.", "# Valid")},
			{Path: "skills/spec-invalid/SKILL.md", Content: "---\nname: spec-invalid\n---\n# Missing description\n"},
			{Path: "skills/malformed/SKILL.md", Content: "not a manifest"},
		},
		Skipped: nil,
	}, nil).Once()

	result, err := ti.service.FetchFromGitHub(ctx, &gen.FetchFromGitHubPayload{
		RepoURL:          repositoryURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "example/repository", result.Repository.FullName)
	require.Equal(t, "abc123", result.Repository.CommitSha)
	require.Len(t, result.Skills, 2)
	require.Equal(t, "valid-skill", result.Skills[0].Name)
	require.True(t, result.Skills[0].SpecValid)
	require.Equal(t, "spec-invalid", result.Skills[1].Name)
	require.False(t, result.Skills[1].SpecValid)
	require.NotEmpty(t, result.Skills[1].ValidationErrors)
	require.Equal(t, []*gen.GitHubSkillIssue{{
		Path:    "skills/malformed/SKILL.md",
		Message: "skill manifest is missing its opening frontmatter delimiter",
	}}, result.Issues)
	ti.repositoryReader.AssertExpectations(t)
}

func TestFetchFromGitHubReportsOversizedSkillsAsIssues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	repositoryURL := "https://github.com/example/repository"
	ti.repositoryReader.On("FetchSkillFiles", mock.Anything, repositoryURL).Return(&ghclient.PublicRepositorySnapshot{
		URL:           repositoryURL,
		FullName:      "example/repository",
		DefaultBranch: "main",
		CommitSHA:     "abc123",
		Visibility:    "public",
		Files: []ghclient.PublicRepositoryFile{
			{Path: "skills/valid/SKILL.md", Content: skillManifest("valid-skill", "A valid skill.", "# Valid")},
		},
		Skipped: []ghclient.PublicRepositorySkippedFile{
			{Path: "skills/huge/SKILL.md", Size: 1024 * 1024},
		},
	}, nil).Once()

	result, err := ti.service.FetchFromGitHub(ctx, &gen.FetchFromGitHubPayload{
		RepoURL:          repositoryURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Skills, 1)
	require.Len(t, result.Issues, 1)
	require.Equal(t, "skills/huge/SKILL.md", result.Issues[0].Path)
	require.Contains(t, result.Issues[0].Message, "exceeds the 65536 byte limit")
	ti.repositoryReader.AssertExpectations(t)
}

func TestFetchFromGitHubMapsEmptyRepositoryToInvalid(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	repositoryURL := "https://github.com/example/empty"
	ti.repositoryReader.On("FetchSkillFiles", mock.Anything, repositoryURL).Return(nil, ghclient.ErrRepositoryEmpty).Once()

	_, err := ti.service.FetchFromGitHub(ctx, &gen.FetchFromGitHubPayload{
		RepoURL:          repositoryURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestFetchFromGitHubMapsTooManySkillFilesToInvalid(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	repositoryURL := "https://github.com/example/huge"
	ti.repositoryReader.On("FetchSkillFiles", mock.Anything, repositoryURL).Return(nil, ghclient.ErrTooManySkillFiles).Once()

	_, err := ti.service.FetchFromGitHub(ctx, &gen.FetchFromGitHubPayload{
		RepoURL:          repositoryURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestFetchFromGitHubMapsArchiveTooLargeToInvalid(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	repositoryURL := "https://github.com/example/large"
	ti.repositoryReader.On("FetchSkillFiles", mock.Anything, repositoryURL).Return(nil, ghclient.ErrRepositoryTooLarge).Once()

	_, err := ti.service.FetchFromGitHub(ctx, &gen.FetchFromGitHubPayload{
		RepoURL:          repositoryURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
