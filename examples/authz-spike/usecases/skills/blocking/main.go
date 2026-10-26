// This executable verifies blocking rules through the production-shaped Get
// handler. Its identity, feature, and repository implementations are fixtures,
// not authentication, entitlement, or database implementations for production.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
	"github.com/speakeasy-api/gram/examples/authz-spike/usecases/skills"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	user := authz.Principal{OrganizationID: "example-org", Kind: authz.UserPrincipal, ID: "example-user"}
	subject := authz.Subject{
		Principal:      user,
		Authentication: authz.Authentication{Kind: authz.SessionAuthentication, ID: "example-session"},
		Delegation:     authz.Delegation{Kind: authz.DirectExecution},
	}
	project := authz.ResourceRef{OrganizationID: user.OrganizationID, Kind: authz.Project, ID: "example-project"}
	skillA := skills.SkillReference{OrganizationID: user.OrganizationID, ProjectID: project.ID, ID: "skill-a"}
	skillB := skills.SkillReference{OrganizationID: user.OrganizationID, ProjectID: project.ID, ID: "skill-b"}
	projectWrite := authz.Grant{Permission: authz.SkillWrite, Anchor: project}
	directRead := authz.Grant{Permission: authz.SkillRead, Anchor: skillA.AuthzTarget().Resource}
	blockRead := authz.Grant{Permission: authz.SkillBlockedRead, Anchor: skillA.AuthzTarget().Resource}
	blockWrite := authz.Grant{Permission: authz.SkillBlockedWrite, Anchor: skillA.AuthzTarget().Resource}

	cases := []struct {
		name    string
		grants  []authz.Grant
		skillID string
		allowed bool
	}{
		{
			name: "project skill write implies read", grants: []authz.Grant{projectWrite},
			skillID: skillA.ID, allowed: true,
		},
		{
			name: "skill read block defeats broader project write", grants: []authz.Grant{blockRead, projectWrite},
			skillID: skillA.ID, allowed: false,
		},
		{
			name: "user's own block defeats their direct skill allow", grants: []authz.Grant{directRead, blockRead},
			skillID: skillA.ID, allowed: false,
		},
		{
			name: "block on skill A does not hide sibling skill B", grants: []authz.Grant{projectWrite, blockRead},
			skillID: skillB.ID, allowed: true,
		},
		{
			name: "write-only block leaves read available", grants: []authz.Grant{projectWrite, blockWrite},
			skillID: skillA.ID, allowed: true,
		},
	}
	for _, example := range cases {
		client, err := authz.NewClient(authz.NewMemoryBackend(), authz.AllDefinitions())
		if err != nil {
			return err
		}
		// Fixture setup assigns every grant directly to the same user. This does
		// not exercise the unimplemented exception to inherited role blocks.
		if err := client.ReplaceGrants(ctx, user, example.grants); err != nil {
			return fmt.Errorf("%s: set fixture policy: %w", example.name, err)
		}
		repository := &fixtureRepository{references: map[string]skills.SkillReference{skillA.ID: skillA, skillB.ID: skillB}, contentReads: 0}
		service := skills.NewService(
			fixtureIdentity{request: skills.AuthenticatedRequest{Subject: subject, ProjectID: project.ID}},
			client, repository, fixtureFeature{enabled: true},
		)
		result, err := service.Get(ctx, example.skillID)
		if example.allowed {
			if err != nil {
				return fmt.Errorf("%s: expected allow: %w", example.name, err)
			}
			if result.ID != example.skillID || repository.contentReads != 1 {
				return fmt.Errorf("%s: expected exactly one authorized content read", example.name)
			}
			fmt.Printf("PASS: %s — allowed, content reads=1\n", example.name)
			continue
		}
		if !errors.Is(err, authz.ErrDenied) {
			return fmt.Errorf("%s: expected authorization denial, got %v", example.name, err)
		}
		if repository.contentReads != 0 || result != (skills.SkillDetail{}) {
			return fmt.Errorf("%s: denial loaded or returned protected content", example.name)
		}
		fmt.Printf("PASS: %s — denied, content reads=0\n", example.name)
	}
	return nil
}

// fixtureIdentity supplies an already trusted session assertion for this example.
// It does not prove a real session or membership and is not a production adapter.
type fixtureIdentity struct {
	request skills.AuthenticatedRequest
}

func (f fixtureIdentity) FromContext(ctx context.Context) (skills.AuthenticatedRequest, error) {
	if err := ctx.Err(); err != nil {
		return skills.AuthenticatedRequest{}, err
	}
	return f.request, nil
}

type fixtureFeature struct {
	enabled bool
}

func (f fixtureFeature) RequireEnabled(ctx context.Context, organizationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.enabled || organizationID != "example-org" {
		return fmt.Errorf("skills are unavailable")
	}
	return nil
}

// fixtureRepository captures protected reads while resolving trusted ancestry
// from stored rows. Request scope cannot overwrite the row's project or tenant.
type fixtureRepository struct {
	references   map[string]skills.SkillReference
	contentReads int
}

func (r *fixtureRepository) GetReference(ctx context.Context, organizationID, projectID, skillID string) (skills.SkillReference, error) {
	if err := ctx.Err(); err != nil {
		return skills.SkillReference{}, err
	}
	reference, exists := r.references[skillID]
	if !exists || reference.OrganizationID != organizationID || reference.ProjectID != projectID {
		return skills.SkillReference{}, fmt.Errorf("skill not found")
	}
	return reference, nil
}

func (r *fixtureRepository) GetDetail(ctx context.Context, reference skills.SkillReference) (skills.SkillDetail, error) {
	if err := ctx.Err(); err != nil {
		return skills.SkillDetail{}, err
	}
	stored, exists := r.references[reference.ID]
	if !exists || stored != reference {
		return skills.SkillDetail{}, fmt.Errorf("skill not found")
	}
	r.contentReads++
	return skills.SkillDetail{ID: stored.ID, Content: "example skill content"}, nil
}
