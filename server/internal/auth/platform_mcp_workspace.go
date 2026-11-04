package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/growthsignals"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// ErrUserHasOrganization refuses a Platform MCP workspace for a user who
// already belongs to an organization. Only a user with no organization may
// create one from the OAuth flow.
var ErrUserHasOrganization = errors.New("user already belongs to an organization")

// ErrUserHasPendingInvitation refuses a Platform MCP workspace for a user who
// has a live invitation to an existing organization. Such a user joins that
// organization instead, so an invited teammate does not split from their team.
var ErrUserHasPendingInvitation = errors.New("user has a pending organization invitation")

// ErrWorkspaceCreationInProgress refuses a Platform MCP workspace while
// another submit for the same user is still creating one.
var ErrWorkspaceCreationInProgress = errors.New("workspace creation already in progress")

// platformMCPWorkspaceLeaseTTL bounds how long a per-user workspace lease is
// held if its holder dies before releasing it. It is well above the time a
// provisioning takes, WorkOS call included.
const platformMCPWorkspaceLeaseTTL = 2 * time.Minute

// platformMCPClientProperty names the MCP client behind a Platform MCP signup
// on its analytics events.
const platformMCPClientProperty = "client"

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
		candidates = append(candidates, name+"'s organization")
	}
	if local != "" {
		candidates = append(candidates, local+"'s organization")
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
// clientName is the registered name of the OAuth client the user is
// connecting. It attributes the signup to that MCP client in analytics.
//
// It returns the organization ID and its stored name.
func (s *Service) ProvisionPlatformMCPWorkspace(ctx context.Context, userID, name, clientName string) (string, string, error) {
	orgName, err := orgprovision.ValidateName(name)
	if err != nil {
		return "", "", err
	}

	user, err := usersRepo.New(s.db).GetUser(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("read user for workspace: %w", err)
	}

	org, err := s.provisionFirstOrgForUser(ctx, userID, orgName, user.Email)
	if err != nil {
		return "", "", err
	}

	if _, err := s.getProjectsOrSetupDefaults(ctx, org.ID); err != nil {
		return "", "", fmt.Errorf("setup default project: %w", err)
	}

	if err := s.trialNotifier.TrialStarted(ctx, org.ID); err != nil {
		s.logger.ErrorContext(ctx, "failed to notify trial started", attr.SlogError(err), attr.SlogOrganizationID(org.ID), attr.SlogUserID(userID))
	}

	s.captureSignupTelemetry(ctx, user.Email, org.Name, org, createdViaPlatformMCP, map[string]string{
		growthsignals.PropertySignupSource: createdViaPlatformMCP,
		platformMCPClientProperty:          clientName,
	})

	return org.ID, org.Name, nil
}

// provisionFirstOrgForUser provisions an organization only while the user
// has none and no pending invitation to one. Two OAuth challenges for the same user can submit at once, so a
// per-user lease makes the check and the provisioning one step: only one of
// them creates a workspace and a trial. The other is refused at once.
//
// The lease lives in Redis, not Postgres, so no database connection is held
// while provisioning calls WorkOS and takes its own connections. A lease that
// outlives its TTL lapses, so a provisioning slower than the TTL is no longer
// serialized.
func (s *Service) provisionFirstOrgForUser(ctx context.Context, userID, orgName, actorEmail string) (orgRepo.OrganizationMetadatum, error) {
	var empty orgRepo.OrganizationMetadatum

	leases, ok := s.nonceStore.(cache.LeaseCache)
	if !ok {
		return empty, errors.New("workspace lock store does not support leases")
	}
	key := "platform-mcp-workspace:" + userID
	owner := uuid.NewString()
	held, err := leases.AcquireLease(ctx, key, owner, platformMCPWorkspaceLeaseTTL)
	if err != nil {
		return empty, fmt.Errorf("acquire workspace lock: %w", err)
	}
	if !held {
		return empty, ErrWorkspaceCreationInProgress
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		released, err := leases.ReleaseLeaseIfOwner(releaseCtx, key, owner)
		switch {
		case err != nil:
			s.logger.ErrorContext(releaseCtx, "failed to release Platform MCP workspace lock", attr.SlogError(err), attr.SlogUserID(userID))
		case !released:
			s.logger.WarnContext(releaseCtx, "Platform MCP workspace lock expired or changed owner before release", attr.SlogUserID(userID))
		}
	}()

	existing, err := s.orgRepo.ListOrganizationsForUser(ctx, conv.ToPGText(userID))
	if err != nil {
		return empty, fmt.Errorf("list user organizations: %w", err)
	}
	if len(existing) > 0 {
		return empty, ErrUserHasOrganization
	}
	invitations, err := s.orgRepo.ListPendingInvitationsForEmail(ctx, conv.NormalizeEmail(actorEmail))
	if err != nil {
		return empty, fmt.Errorf("list pending invitations: %w", err)
	}
	if len(invitations) > 0 {
		return empty, ErrUserHasPendingInvitation
	}

	org, err := s.provisionOrgForUser(ctx, userID, orgName, orgProvisionOptions{
		StripePromotionCodeID: "",
		Whitelisted:           true,
		ProvisionTrial:        true,
		PartnerCode:           "",
		ActorEmail:            actorEmail,
		CreationSource:        orgprovision.SourcePlatformMCP,
	})
	if err != nil {
		return empty, fmt.Errorf("provision workspace: %w", err)
	}
	return org, nil
}
