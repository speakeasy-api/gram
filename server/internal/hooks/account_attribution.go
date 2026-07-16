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

	// accountTypeTeam is a company/enterprise AI account: it lives under a
	// provider org already shared by resolved org members, or its session email
	// resolves to a Gram org member (see classifyAccount).
	accountTypeTeam = "team"
	// accountTypePersonal is an individual AI account (e.g. Claude Max): its
	// provider org is not a recognized enterprise org and its email does not
	// resolve to an org member.
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
// Classification always runs and always stamps AccountType, including for a
// session that carries no provider account UUID (a company-credential session —
// see classifyAccount). The device bridge and the user_accounts entity, however,
// key on that UUID, so they are skipped when it is absent: there is no account
// entity to persist. All failures are returned to the caller, which logs and
// continues: account attribution must never block session capture or enforcement.
func (s *Service) attributeSession(ctx context.Context, meta *SessionMetadata) error {
	// Classify before consulting the device bridge: meta.UserID at this point
	// reflects email resolution only.
	accountType, err := s.classifyAccount(ctx, meta)
	if err != nil {
		return fmt.Errorf("classify account: %w", err)
	}
	meta.AccountType = accountType

	// The device bridge, the user_accounts entity, and its billing mode all key on
	// the provider account UUID (user_accounts.external_account_uuid is NOT NULL
	// and is the entity key). A session authenticated by company credentials (an
	// API key, a gateway/proxy, Bedrock, or Vertex) carries no such UUID, so there
	// is no account entity to persist — the account_type stamped above is the
	// signal the cost surfaces consume. Stop here for these sessions.
	if meta.ExternalAccountUUID == "" {
		return nil
	}

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
//
// The primary signal is deterministic and data-derived: a provider org already
// shared by two or more distinct resolved employees is the company's real
// enterprise org, so every account observed under it is team — including one
// whose own email has not (yet) resolved to a Gram member. This is what lets a
// genuine employee on the company Claude org be classified team before they are
// provisioned in Gram (email resolution alone would call them personal).
//
// When the session's org is not (yet) a shared enterprise org, classification
// falls back to email resolution: a resolved work email is team, anything else
// (a personal email that does not resolve, or no email) is personal. One
// correction rides on top of the resolved case — the work-email guard: a
// resolved email under a solo provider org is downgraded to personal when the
// same employee also appears under a DIFFERENT shared enterprise org, i.e. a
// personal account (e.g. Claude Max) signed in with the work email.
//
// meta.UserID must still reflect email resolution only (call before the device
// bridge can fill it in).
//
// KNOWN RESIDUAL GAP: an org with fewer than two resolved employees cannot be
// recognized as an enterprise org from data alone, so a personal account on a
// work email at a solo/low-adoption company stays labeled team, and an
// unresolved account there stays personal even if it is a real employee. Closing
// this deterministically needs an explicit admin-declared enterprise org id (a
// separate follow-up); accepted because Gram is enterprise software.
func (s *Service) classifyAccount(ctx context.Context, meta *SessionMetadata) (string, error) {
	// A Claude session with no provider account UUID is authenticated by company
	// credentials — an API key, a gateway/proxy, Bedrock, or Vertex — not a
	// personal Claude subscription. A personal Max/Pro account signs in via OAuth
	// and so always emits user.account_uuid (and organization.id); their absence
	// means no personal account is behind the session, so it is a company (team)
	// account. This holds even when the work email has not been provisioned in
	// Gram yet — the whole population an email- or org-based signal would miss for
	// an org that runs Claude Code entirely through a corporate gateway.
	//
	// KNOWN RESIDUAL GAP: user.account_uuid rides only on some event types, so a
	// personal session whose first OTEL batch happens to carry none is classified
	// team for that batch; when the UUID arrives, sessionEnrichesAttribution
	// re-attributes it personal, but the first batch's rows keep the team stamp
	// (telemetry rows are stamped at write). Likewise a client too old to emit
	// user.account_uuid at all is classified team for personal sessions too.
	// Accepted: both slices are small and the prior behavior — an entire
	// company-credential org parked under an unclassified account type — was the
	// far larger distortion.
	if meta.ExternalAccountUUID == "" {
		return accountTypeTeam, nil
	}

	if meta.ExternalOrgID != "" {
		shared, err := s.isSharedEnterpriseOrg(ctx, meta)
		if err != nil {
			return "", fmt.Errorf("evaluate shared enterprise org: %w", err)
		}
		if shared {
			return accountTypeTeam, nil
		}
	}

	if classifyAccountType(meta.UserID) != accountTypeTeam {
		return accountTypePersonal, nil
	}

	// Resolved work email under a non-shared org: guard against a personal account
	// signed in with the employee's work email.
	if meta.ExternalOrgID != "" {
		personal, err := s.employeeAlsoUsesSharedOrg(ctx, meta)
		if err != nil {
			return "", fmt.Errorf("evaluate work-email guard: %w", err)
		}
		if personal {
			return accountTypePersonal, nil
		}
	}
	return accountTypeTeam, nil
}

// isSharedEnterpriseOrg reports whether the session's provider org is already
// shared by two or more distinct resolved employees. Such an org is the company's
// real enterprise org, so any account under it — even one whose email has not
// resolved — is a team account. Requires meta.ExternalOrgID to be non-empty.
func (s *Service) isSharedEnterpriseOrg(ctx context.Context, meta *SessionMetadata) (bool, error) {
	employees, err := s.repo.CountEmployeesForExternalOrg(ctx, repo.CountEmployeesForExternalOrgParams{
		OrganizationID: meta.GramOrgID,
		Provider:       meta.Provider,
		ExternalOrgID:  conv.ToPGTextEmpty(meta.ExternalOrgID),
	})
	if err != nil {
		return false, fmt.Errorf("count employees for external org: %w", err)
	}
	return employees >= 2, nil
}

// employeeAlsoUsesSharedOrg reports whether the resolved employee behind this
// session also appears under a DIFFERENT provider org shared by >= 2 employees
// (the company's real enterprise org). If so, this session's solo provider org is
// almost certainly a personal account signed in with the work email, and should
// not be classified team. Requires meta.UserID and meta.ExternalOrgID to be
// non-empty. This is a best-effort heuristic: it leans team when it cannot tell.
func (s *Service) employeeAlsoUsesSharedOrg(ctx context.Context, meta *SessionMetadata) (bool, error) {
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
	// The account's own email, distinct from user.email (the authenticated
	// actor). Sourced only from ObservedUserEmail — never UserEmail, which on
	// merged canonical metadata holds the actor.
	if meta.ObservedUserEmail != "" {
		attrs[attr.AccountEmailKey] = meta.ObservedUserEmail
	}
}
