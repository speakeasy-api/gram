package hooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

const (
	// providerAnthropic is the provider tag for Claude (Claude Code, Cowork)
	// sessions — the AI-service vendor behind the agent. Personal-account
	// attribution (user_accounts/device_owners) is wired only for Anthropic today;
	// the other providers below tag telemetry for the usage `provider` dimension.
	providerAnthropic = "anthropic"
	// providerOpenAI tags Codex (OpenAI) sessions.
	providerOpenAI = "openai"
	// providerCursor tags Cursor sessions (Cursor brokers multiple model vendors,
	// so the account's provider is Cursor itself).
	providerCursor = "cursor"

	// accountTypeTeam is a company/enterprise AI account: its session email
	// resolves to a Gram org member.
	accountTypeTeam = "team"
	// accountTypePersonal is an individual AI account (e.g. Claude Max) whose
	// email does not resolve to an org member.
	accountTypePersonal = "personal"
)

// classifyAccountType labels a session's AI account from email resolution alone.
// A session whose work email resolved to a Gram org member is a team/enterprise
// account; anything else (a personal email that does not resolve, or no email)
// is personal. Note this is deliberately independent of the device bridge: a
// personal account can be attributed to an employee via their device without
// becoming a team account.
func classifyAccountType(emailResolvedUserID string) string {
	if emailResolvedUserID != "" {
		return accountTypeTeam
	}
	return accountTypePersonal
}

// attributeSession classifies the session's AI account, links it to the owning
// employee, and persists the account entity (user_accounts) plus the device
// bridge (device_owners). It mutates meta in place: AccountType, UserAccountID,
// and — for a personal account attributed through the device bridge — UserID.
//
// It is a no-op when the session carries no provider account identity (e.g. an
// older client that does not emit user.account_uuid), since there is no entity
// to key on. All failures are returned to the caller, which logs and continues:
// account attribution must never block session capture or enforcement.
func (s *Service) attributeSession(ctx context.Context, meta *SessionMetadata) error {
	if meta.ExternalAccountUUID == "" {
		return nil
	}

	// Classify before consulting the device bridge: meta.UserID at this point
	// reflects email resolution only.
	accountType, err := s.classifyAccount(ctx, meta)
	if err != nil {
		return fmt.Errorf("classify account: %w", err)
	}
	meta.AccountType = accountType

	// Teach and resolve the device bridge. A team session (known employee)
	// teaches device -> employee; a personal session (empty UserID) adopts the
	// employee already learned for the device, if any. COALESCE in the query
	// keeps a known owner when this session has none.
	if meta.DeviceID != "" {
		owner, err := s.repo.UpsertDeviceOwner(ctx, repo.UpsertDeviceOwnerParams{
			OrganizationID: meta.GramOrgID,
			Provider:       meta.Provider,
			DeviceID:       meta.DeviceID,
			LinkedUserID:   conv.ToPGTextEmpty(meta.UserID),
		})
		if err != nil {
			return fmt.Errorf("upsert device owner: %w", err)
		}
		if meta.UserID == "" {
			meta.UserID = conv.FromPGTextOrEmpty[string](owner)
		}
	}

	account, err := s.repo.UpsertUserAccount(ctx, repo.UpsertUserAccountParams{
		OrganizationID:      meta.GramOrgID,
		Provider:            meta.Provider,
		ExternalAccountUuid: meta.ExternalAccountUUID,
		UserID:              conv.ToPGTextEmpty(meta.UserID),
		ExternalOrgID:       conv.ToPGTextEmpty(meta.ExternalOrgID),
		ExternalAccountID:   conv.ToPGTextEmpty(meta.ExternalAccountID),
		Email:               conv.ToPGTextEmpty(meta.UserEmail),
		AccountType:         conv.ToPGTextEmpty(meta.AccountType),
	})
	if err != nil {
		return fmt.Errorf("upsert user account: %w", err)
	}
	meta.UserAccountID = account.ID.String()

	billingMode, err := s.resolveBillingMode(ctx, meta, conv.FromPGTextOrEmpty[string](account.BillingMode))
	if err != nil {
		return fmt.Errorf("resolve billing mode: %w", err)
	}
	meta.BillingMode = billingMode

	return nil
}

// providerBillingConfigProvider maps a session's provider tag to the provider
// identifier used on ai_integration_configs, where org-level billing modes are
// declared. Claude sessions tag provider "anthropic", but its integration config
// (the Compliance API integration that carries the external org) is stored under
// "anthropic_compliance". Providers without a distinct config identifier map to
// themselves.
func providerBillingConfigProvider(provider string) string {
	switch provider {
	case providerAnthropic:
		return "anthropic_compliance"
	default:
		return provider
	}
}

// resolveBillingMode walks the billing-mode cascade for a session: an account
// override (user_accounts.billing_mode) wins, else the org-level declaration on
// the provider's AI integration config (matched to the session's external org),
// else empty (treated as unknown — cost is an estimate). accountOverride is the
// value returned by the user_accounts upsert, so no extra query is needed for the
// common case where no override is set.
func (s *Service) resolveBillingMode(ctx context.Context, meta *SessionMetadata, accountOverride string) (string, error) {
	if accountOverride != "" {
		return accountOverride, nil
	}

	orgMode, err := s.repo.GetProviderOrgBillingMode(ctx, repo.GetProviderOrgBillingModeParams{
		OrganizationID: meta.GramOrgID,
		Provider:       providerBillingConfigProvider(meta.Provider),
		ExternalOrgID:  conv.ToPGTextEmpty(meta.ExternalOrgID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get provider org billing mode: %w", err)
	}
	return conv.FromPGTextOrEmpty[string](orgMode), nil
}

// classifyAccount determines whether the session's account is team or personal.
// The base signal is email resolution (classifyAccountType). On top of that it
// applies the work-email guard: a session that resolved to an org member is
// downgraded to personal when it looks like a personal account signed in with
// the employee's work email. meta.UserID must still reflect email resolution
// only (i.e. call before the device bridge can fill it in).
func (s *Service) classifyAccount(ctx context.Context, meta *SessionMetadata) (string, error) {
	base := classifyAccountType(meta.UserID)
	if base != accountTypeTeam || meta.ExternalOrgID == "" {
		return base, nil
	}

	personal, err := s.looksLikePersonalAccountOnWorkEmail(ctx, meta)
	if err != nil {
		return "", fmt.Errorf("evaluate enterprise org membership: %w", err)
	}
	if personal {
		return accountTypePersonal, nil
	}
	return accountTypeTeam, nil
}

// looksLikePersonalAccountOnWorkEmail reports whether a session that resolved to
// an org member (so it classified team on email alone) is actually a personal
// account signed in with the employee's work email. The signal: this provider
// org is solo for the employee (fewer than two distinct employees ever seen
// under it) while the same employee also appears under a DIFFERENT provider org
// shared by >= 2 employees — i.e. the company's real enterprise org.
//
// This is a best-effort heuristic, not a proof. It never downgrades a normal
// employee with a single provider org, and it leans team when it cannot tell.
//
// KNOWN RESIDUAL GAP: a truly solo company — a single employee whose enterprise
// org also has just one member — cannot be distinguished from a personal account
// on a work email, so such a personal account stays labeled team. Accepted
// because Gram is enterprise software and won't be used by solo companies. The
// deterministic fix (an admin-declared enterprise organization.id, matched
// against the stored external_org_id) can close it later if ever needed.
func (s *Service) looksLikePersonalAccountOnWorkEmail(ctx context.Context, meta *SessionMetadata) (bool, error) {
	employees, err := s.repo.CountEmployeesForExternalOrg(ctx, repo.CountEmployeesForExternalOrgParams{
		OrganizationID: meta.GramOrgID,
		Provider:       meta.Provider,
		ExternalOrgID:  conv.ToPGTextEmpty(meta.ExternalOrgID),
	})
	if err != nil {
		return false, fmt.Errorf("count employees for external org: %w", err)
	}
	// A provider org already shared across employees is the enterprise org, not a
	// personal one — never downgrade it.
	if employees >= 2 {
		return false, nil
	}

	hasShared, err := s.repo.EmployeeHasSharedExternalOrg(ctx, repo.EmployeeHasSharedExternalOrgParams{
		OrganizationID: meta.GramOrgID,
		Provider:       meta.Provider,
		UserID:         conv.ToPGTextEmpty(meta.UserID),
		ExternalOrgID:  conv.ToPGTextEmpty(meta.ExternalOrgID),
	})
	if err != nil {
		return false, fmt.Errorf("check employee shared external org: %w", err)
	}
	return hasShared, nil
}

// stampAccountAttribution writes the normalized, provider-agnostic account
// attributes onto a telemetry attribute map so they materialize into the
// corresponding ClickHouse columns. Only non-empty values are written so an
// unclassified or identity-less session leaves the columns empty rather than
// stamping blanks. meta is the zero value when no attribution exists for the
// record's session (a map miss), in which case nothing is stamped.
func stampAccountAttribution(attrs map[attr.Key]any, meta SessionMetadata) {
	if meta.Provider != "" {
		attrs[attr.ProviderKey] = meta.Provider
	}
	if meta.ExternalOrgID != "" {
		attrs[attr.ExternalOrgIDKey] = meta.ExternalOrgID
	}
	if meta.AccountType != "" {
		attrs[attr.AccountTypeKey] = meta.AccountType
	}
	if meta.BillingMode != "" {
		attrs[attr.BillingModeKey] = meta.BillingMode
	}
	if meta.DeviceID != "" {
		attrs[attr.DeviceIDKey] = meta.DeviceID
	}
}
