package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrTypeDNSNotFound is retained for its historical value: older workflow
// histories carry non-retryable application errors with this type. New code
// reports missing DNS as a dns_pending result instead so the registration
// workflow can wait out slow propagation.
const ErrTypeDNSNotFound = "CustomDomainDNSNotFound"

// ErrTypeCustomDomainInvalid tags non-retryable failures: malformed or
// prohibited domains, and rows that vanished or changed hands while the
// long-lived verification workflow was pending.
const ErrTypeCustomDomainInvalid = "CustomDomainInvalid"

func newCustomDomainInvalidError(ctx context.Context, logger *slog.Logger, cause error, message string) error {
	return temporal.NewNonRetryableApplicationError(message, ErrTypeCustomDomainInvalid,
		oops.E(oops.CodeBadRequest, cause, "%s", message).LogError(ctx, logger))
}

// VerifyCustomDomainStatus is the structured outcome of one verification pass.
type VerifyCustomDomainStatus string

const (
	// VerifyStatusVerified means ownership was proven and persisted.
	VerifyStatusVerified VerifyCustomDomainStatus = "verified"
	// VerifyStatusDNSPending means the required records were missing or wrong
	// in a way indistinguishable from in-flight propagation; the workflow
	// should re-check later rather than fail.
	VerifyStatusDNSPending VerifyCustomDomainStatus = "dns_pending"
)

type VerifyCustomDomainResult struct {
	Status VerifyCustomDomainStatus
	// Reason is a short operator-facing note on why verification is pending.
	Reason string
}

type VerifyCustomDomain struct {
	db                  *pgxpool.Pool
	logger              *slog.Logger
	expectedTargetCNAME string
	expectedARecords    []netip.Addr
	audit               *audit.Logger
	resolver            dns.Resolver
}

func NewVerifyCustomDomain(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger, expectedTargetCNAME string, expectedARecords []netip.Addr) *VerifyCustomDomain {
	return &VerifyCustomDomain{
		db:                  db,
		logger:              logger,
		expectedTargetCNAME: expectedTargetCNAME,
		expectedARecords:    expectedARecords,
		audit:               auditLogger,
		resolver:            dns.NewNetResolver(),
	}
}

// SetResolver replaces the DNS resolver. Intended for testing.
func (d *VerifyCustomDomain) SetResolver(r dns.Resolver) {
	d.resolver = r
}

type VerifyCustomDomainArgs struct {
	OrgID  string
	Domain string
	// CustomDomainID pins verification to the row created by CreateDomain.
	// Zero for workflows scheduled before IDs were threaded through; those
	// fall back to a hostname lookup.
	CustomDomainID uuid.UUID
	CreatedBy      urn.Principal
	CreatedByName  *string
	// ProvisionerKind and IPAllowlist are unused since row creation moved to
	// the registration API; retained for Temporal argument compatibility.
	ProvisionerKind k8s.ProvisionerKind
	IPAllowlist     []string
}

// createLegacyDomainRow preserves the pre-v2 contract for workflows started
// by the old registration API, which never created the row itself.
func (d *VerifyCustomDomain) createLegacyDomainRow(ctx context.Context, args VerifyCustomDomainArgs) (customdomainsRepo.CustomDomain, error) {
	var noRow customdomainsRepo.CustomDomain
	dbtx, err := d.db.Begin(ctx)
	if err != nil {
		return noRow, fmt.Errorf("begin legacy custom domain creation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	kind := args.ProvisionerKind
	if kind == "" {
		kind = k8s.ProvisionerKindIngress
	}
	ipAllowlist := args.IPAllowlist
	if ipAllowlist == nil {
		ipAllowlist = []string{}
	}
	cdr := customdomainsRepo.New(dbtx)
	domain, err := cdr.CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  args.OrgID,
		Domain:          args.Domain,
		IngressName:     conv.PtrToPGText(nil),
		CertSecretName:  conv.PtrToPGText(nil),
		ProvisionerKind: string(kind),
		IpAllowlist:     ipAllowlist,
	})
	if err != nil {
		return noRow, fmt.Errorf("create legacy custom domain: %w", err)
	}
	if err := d.audit.LogCustomDomainCreate(ctx, dbtx, audit.LogCustomDomainCreateEvent{
		OrganizationID:   args.OrgID,
		Actor:            args.CreatedBy,
		ActorDisplayName: args.CreatedByName,
		ActorSlug:        nil,
		CustomDomainURN:  urn.NewCustomDomain(domain.ID),
		DomainName:       domain.Domain,
	}); err != nil {
		return noRow, fmt.Errorf("log legacy custom domain creation: %w", err)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return noRow, fmt.Errorf("commit legacy custom domain creation: %w", err)
	}
	return domain, nil
}

// Do runs one verification pass. The registration API creates the pending row
// before the workflow starts; this activity only reads it, proves ownership
// via the _gram TXT record, and is the sole writer of verified=true. A row
// that is missing, deleted, or bound to another organization or hostname
// terminates the workflow instead of retrying: the identity this workflow was
// started for no longer exists — except legacy zero-ID workflows, whose row
// this activity still creates.
func (d *VerifyCustomDomain) Do(ctx context.Context, args VerifyCustomDomainArgs) (VerifyCustomDomainResult, error) {
	var noResult VerifyCustomDomainResult

	if err := customdomains.ValidateDomainName(args.Domain); err != nil {
		return noResult, newCustomDomainInvalidError(ctx, d.logger, err, err.Error())
	}

	repo := customdomainsRepo.New(d.db)

	var domain customdomainsRepo.CustomDomain
	var err error
	if args.CustomDomainID != uuid.Nil {
		domain, err = repo.GetCustomDomainByID(ctx, args.CustomDomainID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return noResult, newCustomDomainInvalidError(ctx, d.logger, err, "custom domain no longer exists")
		case err != nil:
			return noResult, oops.E(oops.CodeUnexpected, err, "failed to get custom domain").LogError(ctx, d.logger)
		}
	} else {
		// Legacy workflows scheduled before CreateDomain owned row creation:
		// the old API relied on this activity to create the row, so a missing
		// row here means creation, not termination. Deletable once pre-v2
		// registrations have drained.
		domain, err = repo.GetCustomDomainByDomain(ctx, args.Domain)
		if errors.Is(err, pgx.ErrNoRows) {
			domain, err = d.createLegacyDomainRow(ctx, args)
		}
		if err != nil {
			return noResult, oops.E(oops.CodeUnexpected, err, "failed to get custom domain").LogError(ctx, d.logger)
		}
	}

	if domain.OrganizationID != args.OrgID || domain.Domain != args.Domain {
		return noResult, newCustomDomainInvalidError(ctx, d.logger, errors.New("custom domain identity mismatch"), "custom domain does not match this registration")
	}

	routingIssue, err := checkCustomDomainRouting(ctx, d.resolver, domain.Domain, d.expectedTargetCNAME, d.expectedARecords)
	if err != nil {
		return noResult, oops.E(oops.CodeUnexpected, err, "failed to find custom domain mapping for %s", domain.Domain).LogError(ctx, d.logger)
	}
	switch routingIssue {
	case customdomains.HealthIssueDNSNotFound:
		// Indistinguishable from in-flight propagation, which can take hours
		// with some providers.
		return VerifyCustomDomainResult{Status: VerifyStatusDNSPending, Reason: "domain DNS records not found"}, nil
	default:
		// Routing shape is advisory at registration time: proxied/CDN domains
		// and forwarding setups legitimately resolve elsewhere. Ownership is
		// proven by the TXT record below.
		if routingIssue != "" {
			d.logger.InfoContext(ctx, "custom domain not resolving to expected target, continuing to ownership check",
				attr.SlogURLDomain(domain.Domain), attr.SlogReason(string(routingIssue)))
		}
	}

	caaIssue, err := checkCustomDomainCAA(ctx, d.resolver, domain.Domain)
	if err != nil {
		return noResult, oops.E(oops.CodeUnexpected, err, "failed to check CAA records for %s", domain.Domain).LogError(ctx, d.logger)
	}
	if caaIssue == customdomains.HealthIssueCAAForbidden {
		d.logger.InfoContext(ctx, "custom domain CAA records do not authorize Let's Encrypt",
			attr.SlogURLDomain(domain.Domain))
		return VerifyCustomDomainResult{
			Status: VerifyStatusDNSPending,
			Reason: fmt.Sprintf("CAA records do not allow Let's Encrypt; add %s", dns.ExpectedLetsEncryptCAA),
		}, nil
	}

	txtName := "_gram." + domain.Domain
	txts, err := d.resolver.LookupTXT(ctx, txtName)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			d.logger.InfoContext(ctx, "custom domain verification TXT record not found, waiting for propagation", attr.SlogURLDomain(domain.Domain), attr.SlogError(err))
			return VerifyCustomDomainResult{Status: VerifyStatusDNSPending, Reason: fmt.Sprintf("TXT record %s not found", txtName)}, nil
		}
		return noResult, oops.E(oops.CodeUnexpected, err, "failed to find TXT record for %s", txtName).LogError(ctx, d.logger)
	}
	expectedTXT := fmt.Sprintf("gram-domain-verify=%s,%s", domain.Domain, args.OrgID)
	if !slices.Contains(txts, expectedTXT) {
		// A present-but-wrong value usually means a stale record still cached
		// somewhere on the propagation path, so it is pending, not fatal.
		d.logger.InfoContext(ctx, "custom domain verification TXT record does not match expected value", attr.SlogURLDomain(domain.Domain))
		return VerifyCustomDomainResult{Status: VerifyStatusDNSPending, Reason: fmt.Sprintf("TXT record %s does not match the expected value", txtName)}, nil
	}

	// The only code path allowed to set verified=true; reconciliation
	// consumes it and manages activation only.
	if _, err := repo.SetCustomDomainVerified(ctx, domain.ID); err != nil {
		return noResult, oops.E(oops.CodeUnexpected, err, "failed to mark custom domain verified").LogError(ctx, d.logger)
	}

	return VerifyCustomDomainResult{Status: VerifyStatusVerified, Reason: ""}, nil
}
