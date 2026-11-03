package customdomains

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// DNSTargetTokenLength is the number of lowercase base32 characters in a
	// DNS target token. Ten characters carry 50 bits of entropy, which keeps
	// collisions across live domains negligible while leaving the per-domain
	// target (<token>.<suffix>) short enough to type.
	DNSTargetTokenLength = 10

	// maxDNSTargetTokenAttempts bounds how many tokens EnsureDNSTargetToken
	// tries before giving up. A collision is already negligible at 50 bits,
	// so needing a third attempt points at a broken token source rather than
	// bad luck.
	maxDNSTargetTokenAttempts = 3

	// maxDNSNameLength is the longest DNS name in presentation form, without
	// the trailing root dot (RFC 1035 section 2.3.4).
	maxDNSNameLength = 253

	// dnsTargetTokenIndex is the partial unique index that keeps tokens unique
	// across live custom domains.
	dnsTargetTokenIndex = "custom_domains_dns_target_token_key"
)

// DNSRoutingMethod is how a custom domain's DNS was last observed routing
// traffic to Speakeasy. Verification and health checks persist it in
// custom_domains.dns_routing_method.
type DNSRoutingMethod string

const (
	// DNSRoutingMethodPerDomainCNAME is a CNAME to the domain's own per-domain
	// target (<token>.<suffix>).
	DNSRoutingMethodPerDomainCNAME DNSRoutingMethod = "per_domain_cname"

	// DNSRoutingMethodSharedCNAME is a CNAME to the shared target that every
	// custom domain may use.
	DNSRoutingMethodSharedCNAME DNSRoutingMethod = "shared_cname"

	// DNSRoutingMethodARecord is address records (an apex A record or a
	// flattened CNAME) that all point at Speakeasy.
	DNSRoutingMethodARecord DNSRoutingMethod = "a_record"

	// DNSRoutingMethodUnknown means no supported routing method was observed:
	// DNS was missing or pointed elsewhere, the check failed, or a proxied
	// domain passed only the HTTPS probe.
	DNSRoutingMethodUnknown DNSRoutingMethod = "unknown"
)

// TxBeginner starts a transaction. On a pool this is a real transaction; on a
// pgx.Tx it is a savepoint, which lets callers retry a statement that failed
// inside their own transaction without aborting it.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// NewDNSTargetToken returns a random DNS target token of DNSTargetTokenLength
// lowercase base32 characters (a-z, 2-7), which is always a valid DNS label.
func NewDNSTargetToken() string {
	// rand.Text returns 26 characters from the uppercase RFC 4648 base32
	// alphabet, so any prefix is uniformly random.
	return strings.ToLower(rand.Text()[:DNSTargetTokenLength])
}

// ParseDNSTargetSuffix normalizes and validates the parent DNS name of
// per-domain targets. An empty or whitespace-only value is valid and disables
// per-domain targets; any other value must be a valid DNS name.
func ParseDNSTargetSuffix(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	suffix := NormalizeDomainName(value)
	// The suffix must leave room for the token label and its separating dot.
	if len(suffix) > maxDNSNameLength-DNSTargetTokenLength-1 || !domainRegex.MatchString(suffix) {
		return "", fmt.Errorf("custom domain target suffix is invalid: %s", value)
	}
	return suffix, nil
}

// DNSTarget returns a domain's per-domain CNAME target (<token>.<suffix>,
// without a trailing dot). It reports false when per-domain targets are
// disabled (empty suffix) or the domain has no token yet.
func DNSTarget(suffix string, token pgtype.Text) (string, bool) {
	if suffix == "" || !token.Valid || token.String == "" {
		return "", false
	}
	return token.String + "." + suffix, true
}

// ParseDNSTarget returns the token of a per-domain target name. name must be
// normalized (lowercase, no trailing dot). It reports false when per-domain
// targets are disabled (empty suffix) or name is not exactly one label under
// suffix shaped like a token (DNSTargetTokenLength lowercase base32
// characters), so infrastructure names under the suffix are not mistaken for
// another domain's target.
func ParseDNSTarget(suffix, name string) (string, bool) {
	if suffix == "" {
		return "", false
	}
	token, ok := strings.CutSuffix(name, "."+suffix)
	if !ok || !isDNSTargetToken(token) {
		return "", false
	}
	return token, true
}

// isDNSTargetToken reports whether label has the shape NewDNSTargetToken
// produces.
func isDNSTargetToken(label string) bool {
	if len(label) != DNSTargetTokenLength {
		return false
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return false
		}
	}
	return true
}

// EnsureDNSTargetToken assigns a DNS target token to the organization's live
// custom domain with the given id when it has none, and returns the domain's
// token. It returns an empty token and no error when the organization has no
// live domain with that id (missing or soft deleted).
//
// Each attempt runs in its own transaction from db, so a token collision on
// the live-row unique index is retried with a new token from newToken without
// aborting a caller's enclosing transaction.
func EnsureDNSTargetToken(ctx context.Context, db TxBeginner, organizationID string, id uuid.UUID, newToken func() string) (string, error) {
	for range maxDNSTargetTokenAttempts {
		token, err := assignDNSTargetToken(ctx, db, organizationID, id, newToken())
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == dnsTargetTokenIndex {
			continue
		}
		if err != nil {
			return "", err
		}
		return token, nil
	}
	return "", fmt.Errorf("assign custom domain dns target token: %d attempts collided", maxDNSTargetTokenAttempts)
}

func assignDNSTargetToken(ctx context.Context, db TxBeginner, organizationID string, id uuid.UUID, token string) (string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin dns target token assignment: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	assigned, err := queries.AssignCustomDomainDNSTargetToken(ctx, repo.AssignCustomDomainDNSTargetTokenParams{
		DnsTargetToken: token,
		ID:             id,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Already assigned (possibly by a concurrent caller) or no longer live.
		assigned, err = queries.GetCustomDomainDNSTargetToken(ctx, repo.GetCustomDomainDNSTargetTokenParams{
			ID:             id,
			OrganizationID: organizationID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("assign custom domain dns target token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit dns target token assignment: %w", err)
	}
	return conv.FromPGTextOrEmpty[string](assigned), nil
}
