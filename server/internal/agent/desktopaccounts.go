package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	providerAnthropic   = "anthropic"
	accountTypeTeam     = "team"
	accountTypePersonal = "personal"
)

// desktopSurfaces are the Claude Desktop apps the device agent reads accounts
// from, named as agentsurface names them.
var desktopSurfaces = []string{"claude-code-desktop", "cowork"}

// desktopAccount is the pair a scan reports for one Claude account: the
// provider org it was used in most recently, and when.
type desktopAccount struct {
	provider    string
	accountUUID string
	orgUUID     string
	lastSeenAt  time.Time
}

// recordDesktopAccounts stores the Claude Desktop accounts a scan found as
// user_accounts rows owned by the enrolled user (DNO-1137).
//
// Desktop reports account x org pairs, but a user_accounts row holds one org,
// so each account keeps the org it was used in most recently. Classification:
//   - team: another employee uses the same provider org (the company's org).
//   - personal: no one else uses the org, and the employee is known to use a
//     shared org, either in this same report or from earlier sightings.
//   - unset: neither holds yet (e.g. the first employee scanned in a pilot), so
//     no verdict is written and an earlier one, if any, is kept.
func (s *Service) recordDesktopAccounts(ctx context.Context, organizationID, userID string, reported []*gen.AIScanAccount) error {
	pairs, err := parseDesktopAccounts(reported, time.Now().UTC())
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return nil
	}
	accounts := latestOrgPerAccount(pairs)

	// Whether any org in this report is shared, over every reported pair rather
	// than only the org each account keeps, so an employee whose company login
	// was last used in a personal org still counts as a company-org member.
	shared := make(map[string]bool, len(pairs))
	reportHasSharedOrg := false
	for _, a := range pairs {
		org := a.orgUUID
		if _, seen := shared[org]; seen {
			continue
		}
		others, err := s.repo.CountOtherEmployeesForExternalOrg(ctx, repo.CountOtherEmployeesForExternalOrgParams{
			OrganizationID: organizationID,
			Provider:       a.provider,
			ExternalOrgID:  conv.ToPGText(org),
			UserID:         conv.ToPGText(userID),
		})
		if err != nil {
			return fmt.Errorf("count employees for provider org: %w", err)
		}
		shared[org] = others > 0
		reportHasSharedOrg = reportHasSharedOrg || others > 0
	}

	for _, a := range accounts {
		accountType := ""
		switch {
		case shared[a.orgUUID]:
			accountType = accountTypeTeam
		case reportHasSharedOrg:
			accountType = accountTypePersonal
		default:
			elsewhere, err := s.repo.EmployeeHasSharedExternalOrg(ctx, repo.EmployeeHasSharedExternalOrgParams{
				OrganizationID: organizationID,
				Provider:       a.provider,
				UserID:         conv.ToPGText(userID),
				ExternalOrgID:  conv.ToPGText(a.orgUUID),
			})
			if err != nil {
				return fmt.Errorf("check employee for a shared provider org: %w", err)
			}
			if elsewhere {
				accountType = accountTypePersonal
			}
		}

		if err := s.repo.UpsertDesktopUserAccount(ctx, repo.UpsertDesktopUserAccountParams{
			OrganizationID:      organizationID,
			Provider:            a.provider,
			ExternalAccountUuid: a.accountUUID,
			UserID:              conv.ToPGText(userID),
			ExternalOrgID:       conv.ToPGText(a.orgUUID),
			AccountType:         conv.ToPGTextEmpty(accountType),
			LastSeenAt:          conv.ToPGTimestamptz(a.lastSeenAt),
		}); err != nil {
			return fmt.Errorf("upsert desktop user account: %w", err)
		}
	}
	return nil
}

// parseDesktopAccounts validates the reported pairs. Pairs from a provider or
// surface this server does not know (a newer agent) are skipped rather than
// failing the rest. A last-seen time ahead of now (device clock skew) is
// clamped to now.
func parseDesktopAccounts(reported []*gen.AIScanAccount, now time.Time) ([]desktopAccount, error) {
	pairs := make([]desktopAccount, 0, len(reported))
	for _, r := range reported {
		if r == nil {
			return nil, errors.New("accounts must not contain null entries")
		}
		if r.Provider != providerAnthropic || !slices.Contains(desktopSurfaces, r.Surface) {
			continue
		}
		seen, err := time.Parse(time.RFC3339, r.LastSeenAt)
		if err != nil {
			return nil, fmt.Errorf("account last_seen_at must be a valid RFC 3339 timestamp: %w", err)
		}
		if seen.After(now) {
			seen = now
		}
		pairs = append(pairs, desktopAccount{
			provider:    r.Provider,
			accountUUID: strings.ToLower(r.AccountUUID),
			orgUUID:     strings.ToLower(r.OrgUUID),
			lastSeenAt:  seen.UTC(),
		})
	}
	return pairs, nil
}

// latestOrgPerAccount keeps, for each account, the org it was used in most
// recently. Ties break on the org id so the choice is stable across scans.
func latestOrgPerAccount(pairs []desktopAccount) []desktopAccount {
	byAccount := make(map[string]desktopAccount, len(pairs))
	for _, a := range pairs {
		key := a.provider + ":" + a.accountUUID
		current, ok := byAccount[key]
		if !ok || a.lastSeenAt.After(current.lastSeenAt) ||
			(a.lastSeenAt.Equal(current.lastSeenAt) && a.orgUUID < current.orgUUID) {
			byAccount[key] = a
		}
	}
	accounts := make([]desktopAccount, 0, len(byAccount))
	for _, a := range byAccount {
		accounts = append(accounts, a)
	}
	slices.SortFunc(accounts, func(a, b desktopAccount) int {
		return cmp.Or(strings.Compare(a.provider, b.provider), strings.Compare(a.accountUUID, b.accountUUID))
	})
	return accounts
}

// connectedUserID resolves an enrolled email to the organization member it
// belongs to, or "" when no connected member has that email.
func (s *Service) connectedUserID(ctx context.Context, organizationID, email string) (string, error) {
	user, err := usersrepo.New(s.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("resolve enrolled user: %w", err)
	default:
		return user.ID, nil
	}
}
