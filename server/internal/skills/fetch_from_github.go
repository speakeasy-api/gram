package skills

import (
	"context"
	"errors"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

func (s *Service) FetchFromGitHub(ctx context.Context, payload *gen.FetchFromGitHubPayload) (*gen.FetchSkillsFromGitHubResult, error) {
	_, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return nil, err
	}

	snapshot, err := s.repos.FetchSkillFiles(ctx, payload.RepoURL)
	if err != nil {
		switch {
		case errors.Is(err, ghclient.ErrInvalidRepositoryURL):
			return nil, oops.E(oops.CodeInvalid, err, "enter a valid public GitHub repository URL")
		case errors.Is(err, ghclient.ErrPublicRepoNotFound):
			return nil, oops.E(oops.CodeNotFound, err, "public GitHub repository not found")
		case errors.Is(err, ghclient.ErrRepositoryEmpty):
			return nil, oops.E(oops.CodeInvalid, err, "the repository has no commits to scan")
		case errors.Is(err, ghclient.ErrTooManySkillFiles):
			return nil, oops.E(oops.CodeInvalid, err, "the repository contains more than %d SKILL.md files", ghclient.MaxRepositorySkills)
		case errors.Is(err, ghclient.ErrRepositoryTooLarge):
			return nil, oops.E(oops.CodeInvalid, err, "the repository archive is too large to scan")
		default:
			return nil, oops.E(oops.CodeGatewayError, err, "unable to read the GitHub repository").LogError(ctx, logger)
		}
	}

	skills := make([]*gen.FetchedGitHubSkill, 0, len(snapshot.Files))
	issues := make([]*gen.GitHubSkillIssue, 0, len(snapshot.Skipped))
	for _, file := range snapshot.Files {
		parsed, parseErr := parseSkillManifest(file.Content)
		if parseErr != nil {
			issues = append(issues, &gen.GitHubSkillIssue{
				Path:    file.Path,
				Message: manifestErrorMessage(parseErr),
			})
			continue
		}

		validationErrors := make([]*types.SkillValidationError, len(parsed.ValidationErrors))
		for i, validationErr := range parsed.ValidationErrors {
			validationErrors[i] = &types.SkillValidationError{
				Code:    validationErr.Code,
				Field:   validationErr.Field,
				Message: validationErr.Message,
			}
		}
		skills = append(skills, &gen.FetchedGitHubSkill{
			Path:             file.Path,
			Content:          parsed.RawContent,
			Name:             parsed.Name,
			DisplayName:      parsed.DisplayName,
			Description:      parsed.Description,
			SpecValid:        parsed.SpecValid,
			ValidationErrors: validationErrors,
		})
	}
	for _, skippedFile := range snapshot.Skipped {
		issues = append(issues, &gen.GitHubSkillIssue{
			Path:    skippedFile.Path,
			Message: fmt.Sprintf("SKILL.md is %d bytes which exceeds the %d byte limit", skippedFile.Size, ghclient.MaxRepositorySkillBytes),
		})
	}

	return &gen.FetchSkillsFromGitHubResult{
		Repository: &gen.FetchedGitHubRepository{
			URL:           snapshot.URL,
			FullName:      snapshot.FullName,
			DefaultBranch: snapshot.DefaultBranch,
			CommitSha:     snapshot.CommitSHA,
			Visibility:    snapshot.Visibility,
		},
		Skills: skills,
		Issues: issues,
	}, nil
}
