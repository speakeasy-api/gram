package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

type githubProvisioner struct{ *ghclient.Client }

func (g githubProvisioner) ForRepository(ctx context.Context, installationID, repositoryID int64, owner, name string) (GitHubRepository, error) {
	client, err := g.Client.ForRepository(ctx, installationID, repositoryID, owner, name)
	if err != nil {
		return nil, fmt.Errorf("scope GitHub repository: %w", err)
	}
	return client, nil
}

// projectRepository only opens a verified binding that exactly matches the
// requested destination. Reads never provision a repository.
func (s *Service) projectRepository(ctx context.Context, projectID uuid.UUID, owner, name string) (GitHubRepository, error) {
	client, _, err := s.resolveProjectRepository(ctx, projectID, owner, name, false)
	return client, err
}

// resolveProjectRepository never binds a pre-existing GitHub repository by
// name. Only the identity from a successful creation can authorize future I/O.
// When there is no GitHub connection yet, an earlier failed publish may have
// already recorded the destination (including a suffix); the binding wins.
func (s *Service) resolveProjectRepository(ctx context.Context, projectID uuid.UUID, owner, name string, allowCreate bool) (GitHubRepository, string, error) {
	installationID := s.github.InstallationID
	binding, err := s.repo.GetPluginRepositoryBinding(ctx, projectID)
	switch {
	case err == nil:
		if allowCreate {
			name = binding.RepoName
		}
		if binding.InstallationID != installationID || !strings.EqualFold(binding.RepoOwner, owner) || !strings.EqualFold(binding.RepoName, name) {
			return nil, "", fmt.Errorf("%w: repository binding does not match the requested destination", ErrGitHubRepoConflict)
		}
		client, err := s.github.Client.ForRepository(ctx, installationID, binding.RepositoryID, owner, name)
		if err != nil {
			return nil, "", fmt.Errorf("verify bound GitHub repository: %w", err)
		}
		return client, name, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, "", fmt.Errorf("get repository binding: %w", err)
	case !allowCreate:
		return nil, "", fmt.Errorf("%w: no verified repository binding", ErrGitHubRepoConflict)
	}
	const maxNameLen = 100
	name = name[:min(len(name), maxNameLen)]
	id, err := s.github.Client.CreatePrivateRepository(ctx, installationID, owner, name)
	if errors.Is(err, ghclient.ErrRepositoryNameTaken) {
		suffix := "-" + projectID.String()
		name = name[:min(len(name), maxNameLen-len(suffix))] + suffix
		id, err = s.github.Client.CreatePrivateRepository(ctx, installationID, owner, name)
	}
	if err != nil {
		return nil, "", fmt.Errorf("create private repository: %w", err)
	}
	// No upsert: neither a retry nor a concurrent publish may replace a binding.
	err = s.repo.InsertPluginRepositoryBinding(ctx, repo.InsertPluginRepositoryBindingParams{
		ProjectID:      projectID,
		InstallationID: installationID,
		RepositoryID:   id,
		RepoOwner:      owner,
		RepoName:       name,
	})
	if err != nil {
		return nil, "", fmt.Errorf("record new repository identity: %w", err)
	}
	client, err := s.github.Client.ForRepository(ctx, installationID, id, owner, name)
	if err != nil {
		return nil, "", fmt.Errorf("verify newly bound GitHub repository: %w", err)
	}
	return client, name, nil
}
