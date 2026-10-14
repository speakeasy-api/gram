package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// ErrUserHasOrganization refuses a Platform MCP workspace for a user who
// already belongs to an organization. Only a user with no organization may
// create one from the OAuth flow.
var ErrUserHasOrganization = errors.New("user already belongs to an organization")

// freeMailDomains are personal mailbox providers. Their domain names a mail
// service, not the user's company, so a workspace name suggested from one
// would be wrong for every user on it.
var freeMailDomains = map[string]struct{}{
	"aol.com":        {},
	"gmail.com":      {},
	"gmx.com":        {},
	"googlemail.com": {},
	"hey.com":        {},
	"hotmail.com":    {},
	"icloud.com":     {},
	"live.com":       {},
	"mac.com":        {},
	"mail.com":       {},
	"me.com":         {},
	"msn.com":        {},
	"outlook.com":    {},
	"pm.me":          {},
	"proton.me":      {},
	"protonmail.com": {},
	"qq.com":         {},
	"yahoo.com":      {},
	"yandex.com":     {},
	"zoho.com":       {},
}

// PlatformMCPWorkspaceNameSuggestion returns the name the Platform MCP
// workspace form starts with: the company from a work email domain, or the
// user's own name for a personal mailbox. An empty result leaves the field
// blank.
func (s *Service) PlatformMCPWorkspaceNameSuggestion(ctx context.Context, userID string) (string, error) {
	user, err := usersRepo.New(s.db).GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("read user for workspace name: %w", err)
	}
	return suggestWorkspaceName(user.Email, user.DisplayName), nil
}

func suggestWorkspaceName(email, displayName string) string {
	local, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	candidates := make([]string, 0, 3)
	if _, free := freeMailDomains[domain]; domain != "" && !free {
		candidates = append(candidates, capitalizeFirst(orgprovision.NameFromHostname(domain)))
	}
	if name := strings.TrimSpace(displayName); name != "" {
		candidates = append(candidates, name+"'s workspace")
	}
	if local != "" {
		candidates = append(candidates, local+"'s workspace")
	}
	for _, candidate := range candidates {
		if name, err := orgprovision.ValidateName(candidate); err == nil {
			return name
		}
	}
	return ""
}

func capitalizeFirst(value string) string {
	first, size := utf8.DecodeRuneInString(value)
	if first == utf8.RuneError {
		return value
	}
	return string(unicode.ToUpper(first)) + value[size:]
}

// ProvisionPlatformMCPWorkspace creates the first organization for a user who
// signed in through the Platform MCP OAuth flow and belongs to none. It runs
// the same provisioning as a dashboard signup: the WorkOS organization, the
// user as organization admin, the default entitlements (which include
// platform_mcp), and the 14-day enterprise trial, in one transaction. It then
// creates the Default project, so the agent has a project to work in straight
// away.
//
// It returns the organization ID and its stored name.
func (s *Service) ProvisionPlatformMCPWorkspace(ctx context.Context, userID, name string) (string, string, error) {
	orgName, err := orgprovision.ValidateName(name)
	if err != nil {
		return "", "", err
	}

	user, err := usersRepo.New(s.db).GetUser(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("read user for workspace: %w", err)
	}

	existing, err := s.orgRepo.ListOrganizationsForUser(ctx, conv.ToPGText(userID))
	if err != nil {
		return "", "", fmt.Errorf("list user organizations: %w", err)
	}
	if len(existing) > 0 {
		return "", "", ErrUserHasOrganization
	}

	org, err := s.provisionOrgForUser(ctx, userID, orgName, orgProvisionOptions{
		Whitelisted:    true,
		ProvisionTrial: true,
		ActorEmail:     user.Email,
		CreationSource: orgprovision.SourcePlatformMCP,
	})
	if err != nil {
		return "", "", fmt.Errorf("provision workspace: %w", err)
	}

	if _, err := s.getProjectsOrSetupDefaults(ctx, org.ID); err != nil {
		return "", "", fmt.Errorf("setup default project: %w", err)
	}

	if err := s.trialNotifier.TrialStarted(ctx, org.ID); err != nil {
		s.logger.ErrorContext(ctx, "failed to notify trial started", attr.SlogError(err), attr.SlogOrganizationID(org.ID), attr.SlogUserID(userID))
	}

	return org.ID, org.Name, nil
}
