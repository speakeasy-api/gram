package mcptoolexecution

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AuthenticatedUserPrincipalAdapter canonicalizes concrete Gram user
// principals and derives candidates only from authoritative user-session
// provenance. Membership is revalidated against the organization on every
// derivation and is never cached, so a removed member stops producing
// candidates on their next call.
type AuthenticatedUserPrincipalAdapter struct {
	db *pgxpool.Pool
}

// NewAuthenticatedUserPrincipalAdapter builds the concrete-user principal
// adapter.
func NewAuthenticatedUserPrincipalAdapter(db *pgxpool.Pool) *AuthenticatedUserPrincipalAdapter {
	return &AuthenticatedUserPrincipalAdapter{db: db}
}

var _ killswitches.PrincipalAdapter = (*AuthenticatedUserPrincipalAdapter)(nil)

// Kind returns the concrete user principal namespace.
func (a *AuthenticatedUserPrincipalAdapter) Kind() killswitches.PrincipalKind {
	return PrincipalKindUser
}

// Canonicalize trims surrounding whitespace and accepts the user ID verbatim.
// IDs are opaque and case-sensitive, so no folding is applied. Input that can
// never name a user — empty, control characters, embedded whitespace edges,
// or unbounded length — is deliberately unsupported, not an error.
func (a *AuthenticatedUserPrincipalAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.PrincipalKey], error) {
	key, ok := canonicalUserKey(input)
	if !ok {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.PrincipalKey](), nil
	}
	result, err := killswitches.NewCanonicalizationResult(key)
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.PrincipalKey]{}, fmt.Errorf("canonicalize user key: %w", err)
	}
	return result, nil
}

// ValidateCurrentOrganization reports whether the key names a non-deleted
// user with an active membership in the organization. A malformed key is not
// current; only query failures are errors.
func (a *AuthenticatedUserPrincipalAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID killswitches.OrganizationID, key killswitches.PrincipalKey) (bool, error) {
	canonical, ok := canonicalUserKey(string(key))
	if !ok || canonical != key || organizationID == "" {
		return false, nil
	}
	member, err := orgrepo.New(a.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID:         string(key),
		OrganizationID: string(organizationID),
	})
	if err != nil {
		return false, fmt.Errorf("check active organization membership: %w", err)
	}
	return member, nil
}

// DeriveCandidates accepts only mcpidentity.Identity provenance stamped by a
// serving surface after credential validation. Checkpoints call it only when
// mcpidentity.FromContext returns ok: unstamped traffic is classified
// unattributed at the checkpoint and never reaches derivation — that skip is
// the authoritative unsupported/no-candidate outcome for such traffic, not a
// failure. Any non-Identity source here is therefore a checkpoint wiring bug
// and errors.
//
// Only KindUserSession claims an authoritative acting user; that claim is
// then revalidated as an active organization membership before producing the
// single concrete candidate. Anonymous, API-key, assistant, and chat-session
// provenance are deliberately unsupported. A stamped identity with a zero or
// unknown kind, a malformed authoritative claim, or a membership lookup
// failure is an error and follows the definition's fail-closed policy.
func (a *AuthenticatedUserPrincipalAdapter) DeriveCandidates(ctx context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.PrincipalCandidateResult, error) {
	identity, ok := source.(mcpidentity.Identity)
	if !ok {
		return killswitches.PrincipalCandidateResult{}, fmt.Errorf("unsupported principal source type %T", source)
	}
	if organizationID == "" {
		return killswitches.PrincipalCandidateResult{}, fmt.Errorf("organization ID is required")
	}

	switch identity.Kind() {
	case mcpidentity.KindUserSession, mcpidentity.KindDelegatedUser:
		userID := identity.UserID()
		key, canonical := canonicalUserKey(userID)
		if !canonical || string(key) != userID {
			return killswitches.PrincipalCandidateResult{}, fmt.Errorf("authoritative user provenance carries a non-canonical user ID")
		}
		member, err := a.ValidateCurrentOrganization(ctx, organizationID, key)
		if err != nil {
			return killswitches.PrincipalCandidateResult{}, fmt.Errorf("revalidate active organization membership: %w", err)
		}
		if !member {
			return killswitches.UnsupportedPrincipalCandidateResult(), nil
		}
		result, err := killswitches.NewPrincipalCandidateResult([]killswitches.PrincipalCandidate{{Kind: PrincipalKindUser, Key: key}})
		if err != nil {
			return killswitches.PrincipalCandidateResult{}, fmt.Errorf("build principal candidate: %w", err)
		}
		return result, nil
	case mcpidentity.KindAnonymous, mcpidentity.KindAPIKey, mcpidentity.KindAssistant, mcpidentity.KindChatSession:
		return killswitches.UnsupportedPrincipalCandidateResult(), nil
	default:
		return killswitches.PrincipalCandidateResult{}, fmt.Errorf("unknown identity provenance kind %q", identity.Kind())
	}
}

func canonicalUserKey(input string) (killswitches.PrincipalKey, bool) {
	if !utf8.ValidString(input) {
		return "", false
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || len(trimmed) > urn.MaxSessionSubjectIDLength {
		return "", false
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return killswitches.PrincipalKey(trimmed), true
}
