package customdomains_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

var dnsTargetTokenPattern = regexp.MustCompile(`^[a-z2-7]{10}$`)

const (
	// concurrentAssignerBlockedWait is how long the concurrency test expects
	// the second assigner to stay blocked on the row lock. A local UPDATE
	// returns in milliseconds, so still waiting after this long means blocked.
	concurrentAssignerBlockedWait = 200 * time.Millisecond

	// concurrentAssignerDoneWait bounds how long the second assigner may take
	// once the lock is released, so a regression fails instead of hanging.
	concurrentAssignerDoneWait = 10 * time.Second
)

// tokenSequence returns a token source that yields tokens in order and fails
// the test if called more often, plus a pointer to its call count.
func tokenSequence(t *testing.T, tokens ...string) (func() string, *int) {
	t.Helper()

	calls := 0
	return func() string {
		require.Less(t, calls, len(tokens), "token source called more often than expected")
		token := tokens[calls]
		calls++
		return token
	}, &calls
}

func TestNewDNSTargetToken_IsLowercaseBase32Label(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for range 100 {
		token := customdomains.NewDNSTargetToken()
		require.Regexp(t, dnsTargetTokenPattern, token)
		seen[token] = struct{}{}
	}
	require.Len(t, seen, 100)
}

func TestParseDNSTargetSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "empty disables targets", value: "", want: "", wantErr: false},
		{name: "whitespace disables targets", value: "  ", want: "", wantErr: false},
		{name: "plain name", value: "domains.speakeasy.com", want: "domains.speakeasy.com", wantErr: false},
		{name: "normalizes case and trailing dot", value: " Dev.Domains.Speakeasy.com. ", want: "dev.domains.speakeasy.com", wantErr: false},
		{name: "rejects invalid label", value: "-bad.example.com", want: "", wantErr: true},
		{name: "rejects lone root dot", value: ".", want: "", wantErr: true},
		{name: "rejects single label", value: "localhost", want: "", wantErr: true},
		{name: "accepts longest name that leaves room for a token label", value: strings.Repeat("a.", 119) + "abcd", want: strings.Repeat("a.", 119) + "abcd", wantErr: false},
		{name: "rejects name one character too long for a token label", value: strings.Repeat("a.", 119) + "abcde", want: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := customdomains.ParseDNSTargetSuffix(tc.value)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDNSTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		suffix string
		token  pgtype.Text
		want   string
		wantOK bool
	}{
		{name: "suffix and token", suffix: "domains.speakeasy.com", token: conv.ToPGText("abcde23456"), want: "abcde23456.domains.speakeasy.com", wantOK: true},
		{name: "suffix unset", suffix: "", token: conv.ToPGText("abcde23456"), want: "", wantOK: false},
		{name: "token missing", suffix: "domains.speakeasy.com", token: pgtype.Text{String: "", Valid: false}, want: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := customdomains.DNSTarget(tc.suffix, tc.token)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEnsureDNSTargetToken_AssignsMissingToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-assign", Domain: "assign.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	require.False(t, domain.DnsTargetToken.Valid)

	source, calls := tokenSequence(t, "assign2345")
	token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, domain.ID, source)
	require.NoError(t, err)
	require.Equal(t, "assign2345", token)
	require.Equal(t, 1, *calls)

	stored, err := ti.repo.GetCustomDomainDNSTargetToken(ctx, cdrepo.GetCustomDomainDNSTargetTokenParams{ID: domain.ID, OrganizationID: domain.OrganizationID})
	require.NoError(t, err)
	require.Equal(t, "assign2345", stored.String)
}

func TestEnsureDNSTargetToken_KeepsExistingToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-keep", Domain: "keep.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)

	first, _ := tokenSequence(t, "first23456")
	_, err = customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, domain.ID, first)
	require.NoError(t, err)

	second, _ := tokenSequence(t, "second2345")
	token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, domain.ID, second)
	require.NoError(t, err)
	require.Equal(t, "first23456", token)
}

func TestEnsureDNSTargetToken_RetriesOnCollision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	existing, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-existing", Domain: "existing.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	taken, _ := tokenSequence(t, "taken23456")
	_, err = customdomains.EnsureDNSTargetToken(ctx, ti.conn, existing.OrganizationID, existing.ID, taken)
	require.NoError(t, err)

	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-retry", Domain: "retry.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)

	source, calls := tokenSequence(t, "taken23456", "fresh23456")
	token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, domain.ID, source)
	require.NoError(t, err)
	require.Equal(t, "fresh23456", token)
	require.Equal(t, 2, *calls)
}

func TestEnsureDNSTargetToken_GivesUpAfterRepeatedCollisions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	existing, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-existing", Domain: "existing.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	taken, _ := tokenSequence(t, "taken23456")
	_, err = customdomains.EnsureDNSTargetToken(ctx, ti.conn, existing.OrganizationID, existing.ID, taken)
	require.NoError(t, err)

	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-exhaust", Domain: "exhaust.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)

	source, calls := tokenSequence(t, "taken23456", "taken23456", "taken23456")
	_, err = customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, domain.ID, source)
	require.ErrorContains(t, err, "attempts collided")
	require.Equal(t, 3, *calls)

	stored, err := ti.repo.GetCustomDomainDNSTargetToken(ctx, cdrepo.GetCustomDomainDNSTargetTokenParams{ID: domain.ID, OrganizationID: domain.OrganizationID})
	require.NoError(t, err)
	require.False(t, stored.Valid)
}

func TestEnsureDNSTargetToken_CollisionKeepsCallerTransactionUsable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	existing, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-existing", Domain: "existing.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	taken, _ := tokenSequence(t, "taken23456")
	_, err = customdomains.EnsureDNSTargetToken(ctx, ti.conn, existing.OrganizationID, existing.ID, taken)
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction tests that a token collision rolls back only its savepoint
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	domain, err := cdrepo.New(tx).CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-tx", Domain: "tx.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)

	source, _ := tokenSequence(t, "taken23456", "fresh23456")
	token, err := customdomains.EnsureDNSTargetToken(ctx, tx, domain.OrganizationID, domain.ID, source)
	require.NoError(t, err)
	require.Equal(t, "fresh23456", token)

	// The collided attempt rolled back to its savepoint, so the caller's
	// transaction still accepts statements and commits the insert.
	require.NoError(t, tx.Commit(ctx))
	stored, err := ti.repo.GetCustomDomainDNSTargetToken(ctx, cdrepo.GetCustomDomainDNSTargetTokenParams{ID: domain.ID, OrganizationID: domain.OrganizationID})
	require.NoError(t, err)
	require.Equal(t, "fresh23456", stored.String)
}

func TestEnsureDNSTargetToken_ConcurrentAssignerReturnsCommittedToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-concurrent", Domain: "concurrent.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: an open caller-owned transaction holds the row lock that the concurrent assigner must wait on
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	first, _ := tokenSequence(t, "aaaaa23456")
	token, err := customdomains.EnsureDNSTargetToken(ctx, tx, domain.OrganizationID, domain.ID, first)
	require.NoError(t, err)
	require.Equal(t, "aaaaa23456", token)

	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, domain.ID, func() string { return "bbbbb23456" })
		done <- result{token: token, err: err}
	}()

	// The second assigner waits on the row lock held by the open transaction.
	select {
	case got := <-done:
		t.Fatalf("concurrent assigner returned before the first transaction committed: %q, %v", got.token, got.err)
	case <-time.After(concurrentAssignerBlockedWait):
	}

	require.NoError(t, tx.Commit(ctx))

	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Equal(t, "aaaaa23456", got.token)
	case <-time.After(concurrentAssignerDoneWait):
		t.Fatal("concurrent assigner did not return after the first transaction committed")
	}

	stored, err := ti.repo.GetCustomDomainDNSTargetToken(ctx, cdrepo.GetCustomDomainDNSTargetTokenParams{ID: domain.ID, OrganizationID: domain.OrganizationID})
	require.NoError(t, err)
	require.Equal(t, "aaaaa23456", stored.String)
}

func TestEnsureDNSTargetToken_SkipsDomainThatIsNotLive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-deleted", Domain: "deleted.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	require.NoError(t, ti.repo.DeleteCustomDomain(ctx, "org-ensure-deleted"))

	for _, id := range []uuid.UUID{domain.ID, uuid.New()} {
		source, _ := tokenSequence(t, "unused2345")
		token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, domain.OrganizationID, id, source)
		require.NoError(t, err)
		require.Empty(t, token)
	}

	tombstone, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.False(t, tombstone.DnsTargetToken.Valid)
}

func TestEnsureDNSTargetToken_SkipsDomainOfAnotherOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-ensure-owner", Domain: "owner.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)

	source, _ := tokenSequence(t, "other23456")
	token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, "org-ensure-other", domain.ID, source)
	require.NoError(t, err)
	require.Empty(t, token)

	stored, err := ti.repo.GetCustomDomainDNSTargetToken(ctx, cdrepo.GetCustomDomainDNSTargetTokenParams{ID: domain.ID, OrganizationID: domain.OrganizationID})
	require.NoError(t, err)
	require.False(t, stored.Valid)
}

func TestDeleteCustomDomain_FreesTokenForReuse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	deleted, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-free-deleted", Domain: "freed.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	first, _ := tokenSequence(t, "reused2345")
	_, err = customdomains.EnsureDNSTargetToken(ctx, ti.conn, deleted.OrganizationID, deleted.ID, first)
	require.NoError(t, err)

	require.NoError(t, ti.repo.DeleteCustomDomain(ctx, "org-free-deleted"))
	tombstone, err := ti.repo.GetCustomDomainRouteConfig(ctx, deleted.ID)
	require.NoError(t, err)
	require.True(t, tombstone.Deleted)
	require.False(t, tombstone.DnsTargetToken.Valid)

	successor, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: "org-free-successor", Domain: "successor.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	second, calls := tokenSequence(t, "reused2345")
	token, err := customdomains.EnsureDNSTargetToken(ctx, ti.conn, successor.OrganizationID, successor.ID, second)
	require.NoError(t, err)
	require.Equal(t, "reused2345", token)
	require.Equal(t, 1, *calls)
}

func TestCreateDomain_AssignsDNSTargetToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "token.example.com"})
	require.NoError(t, err)

	domain, err := ti.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, domain.DnsTargetToken.Valid)
	require.Regexp(t, dnsTargetTokenPattern, domain.DnsTargetToken.String)
}

func TestCreateDomain_ReverifyBackfillsDNSTargetToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	existing, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: authCtx.ActiveOrganizationID, Domain: "reverify.example.com", IpAllowlist: []string{}})
	require.NoError(t, err)
	require.False(t, existing.DnsTargetToken.Valid)

	_, err = ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "reverify.example.com"})
	require.NoError(t, err)

	domain, err := ti.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, existing.ID, domain.ID)
	require.Regexp(t, dnsTargetTokenPattern, domain.DnsTargetToken.String)
}

func TestDeleteDomain_ClearsDNSTargetToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	_, err := ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "cleared.example.com"})
	require.NoError(t, err)
	domain, err := ti.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, domain.DnsTargetToken.Valid)

	require.NoError(t, ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{SessionToken: nil}))

	tombstone, err := ti.repo.GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.True(t, tombstone.Deleted)
	require.False(t, tombstone.DnsTargetToken.Valid)
}
