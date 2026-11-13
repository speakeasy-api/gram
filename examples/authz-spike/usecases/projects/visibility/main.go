// This executable checks list/detail agreement using explicit local fixtures.
// It does not implement real session admission, database lookup, or pagination.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
	"github.com/speakeasy-api/gram/examples/authz-spike/usecases/projects"
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
		Principal: user, Authentication: authz.Authentication{Kind: authz.SessionAuthentication, ID: "example-session"},
		Delegation: authz.Delegation{Kind: authz.DirectExecution},
	}
	a := projects.Project{OrganizationID: user.OrganizationID, ID: "project-a", Name: "Project A"}
	b := projects.Project{OrganizationID: user.OrganizationID, ID: "project-b", Name: "Project B"}
	c := projects.Project{OrganizationID: user.OrganizationID, ID: "project-c", Name: "Project C"}
	other := projects.Project{OrganizationID: "other-org", ID: a.ID, Name: "Other tenant's project"}
	repository := fixtureRepository{rows: []projects.Project{c, b, a, other}}

	// Application startup constructs one client with the full modeled catalog.
	// No handler selects definitions or initializes a client.
	client, err := authz.NewClient(authz.NewMemoryBackend(), authz.AllDefinitions())
	if err != nil {
		return err
	}
	service := projects.NewService(client, repository)
	readA := authz.Grant{Permission: authz.ProjectRead, Anchor: a.AuthzTarget().Resource}
	writeB := authz.Grant{Permission: authz.ProjectWrite, Anchor: b.AuthzTarget().Resource}
	blockBRead := authz.Grant{Permission: authz.ProjectBlockedRead, Anchor: b.AuthzTarget().Resource}
	blockBWrite := authz.Grant{Permission: authz.ProjectBlockedWrite, Anchor: b.AuthzTarget().Resource}
	organization := authz.ResourceRef{OrganizationID: user.OrganizationID, Kind: authz.Organization, ID: user.OrganizationID}
	cases := []struct {
		name   string
		grants []authz.Grant
		want   []string
	}{
		{
			name:   "A allowed, B blocked, C has no grant",
			grants: []authz.Grant{readA, writeB, blockBRead}, want: []string{a.ID},
		},
		{
			name:   "organization project read minus B, preserving repository order",
			grants: []authz.Grant{{Permission: authz.ProjectRead, Anchor: organization}, blockBRead}, want: []string{c.ID, a.ID},
		},
		{
			name:   "B write implies read; write-only block retains visibility",
			grants: []authz.Grant{writeB, blockBWrite}, want: []string{b.ID},
		},
		{
			name:   "org admin alone is not project read",
			grants: []authz.Grant{{Permission: authz.OrgAdmin, Anchor: organization}}, want: []string{},
		},
		{
			name:   "removing the last direct grants leaves no projects",
			grants: nil, want: []string{},
		},
	}
	for _, example := range cases {
		if err := client.ReplaceGrants(ctx, user, example.grants); err != nil {
			return fmt.Errorf("%s: set fixture policy: %w", example.name, err)
		}
		visible, err := service.List(ctx, subject)
		if err != nil {
			return fmt.Errorf("%s: list: %w", example.name, err)
		}
		ids := make([]string, 0, len(visible))
		for _, row := range visible {
			if row.OrganizationID != user.OrganizationID {
				return fmt.Errorf("%s: list crossed organization boundary", example.name)
			}
			ids = append(ids, row.ID)
		}
		if !slices.Equal(ids, example.want) {
			return fmt.Errorf("%s: want %v, got %v", example.name, example.want, ids)
		}
		// The three detail requests must agree with the list, using the actual
		// handlers and the same unchanged policy, not a duplicated grant evaluator.
		for _, row := range []projects.Project{a, b, c} {
			detail, err := service.Get(ctx, subject, row.ID)
			if slices.Contains(example.want, row.ID) {
				if err != nil || detail != row {
					return fmt.Errorf("%s: visible project %s failed detail check: %v", example.name, row.ID, err)
				}
			} else if !errors.Is(err, authz.ErrDenied) || detail != (projects.Project{}) {
				return fmt.Errorf("%s: hidden project %s was not refused by authorization: %v", example.name, row.ID, err)
			}
		}
		fmt.Printf("PASS: %s — visible=%v; all detail checks agree\n", example.name, ids)
	}
	return nil
}

// fixtureRepository applies tenant scope to stored rows, not grant policy.
type fixtureRepository struct {
	rows []projects.Project
}

func (r fixtureRepository) List(ctx context.Context, organizationID string) ([]projects.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows := []projects.Project{}
	for _, row := range r.rows {
		if row.OrganizationID == organizationID {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (r fixtureRepository) Get(ctx context.Context, organizationID, projectID string) (projects.Project, error) {
	if err := ctx.Err(); err != nil {
		return projects.Project{}, err
	}
	for _, row := range r.rows {
		if row.OrganizationID == organizationID && row.ID == projectID {
			return row, nil
		}
	}
	return projects.Project{}, fmt.Errorf("project not found")
}
