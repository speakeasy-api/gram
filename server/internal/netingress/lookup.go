package netingress

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/netingress/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// gatewayRuntime is networkingress.RuntimeGateway, the only runtime the
// listener serves.
const gatewayRuntime = "gateway"

var (
	ErrIngressUnavailable = errors.New("private network ingress unavailable")
	ErrIngressChanged     = errors.New("private network ingress changed")
)

type Ingress struct {
	ID               uuid.UUID
	OrganizationID   string
	Provider         string
	DNSName          string
	IdentityRequired bool
}

type ingressRepository interface {
	GetActiveIngressesByDNSName(context.Context, string) ([]repo.GetActiveIngressesByDNSNameRow, error)
	GetIngressServingState(context.Context, repo.GetIngressServingStateParams) (repo.GetIngressServingStateRow, error)
}

type IngressLookup struct {
	repo ingressRepository
}

func NewIngressLookup(db repo.DBTX) *IngressLookup {
	return &IngressLookup{repo: repo.New(db)}
}

// ByDNSName resolves the gateway-hosted ingress serving host. Exactly one
// enabled gateway ingress may own a DNS name; anything else fails closed.
func (l *IngressLookup) ByDNSName(ctx context.Context, host string) (Ingress, error) {
	canonical, err := requestorigin.CanonicalHost(host)
	if err != nil || canonical == "" {
		return Ingress{}, fmt.Errorf("%w: invalid request host", ErrIngressUnavailable)
	}
	rows, err := l.repo.GetActiveIngressesByDNSName(ctx, canonical)
	if err != nil {
		return Ingress{}, fmt.Errorf("lookup private network ingress by host: %w", err)
	}
	if len(rows) != 1 {
		return Ingress{}, ErrIngressUnavailable
	}
	return ingressFromRow(rows[0])
}

// Recheck compares every field that can alter serving authority. This does not
// rely on writers remembering to update a timestamp when changing policy.
func (l *IngressLookup) Recheck(ctx context.Context, cached Ingress) error {
	row, err := l.repo.GetIngressServingState(ctx, repo.GetIngressServingStateParams{
		ID:             cached.ID,
		OrganizationID: cached.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIngressUnavailable
	}
	if err != nil {
		return fmt.Errorf("recheck private network ingress: %w", err)
	}
	// ByDNSName serves only gateway rows, so a row whose runtime changed no
	// longer serves its cached host either.
	if !row.Enabled || row.Deleted || row.Runtime.String != gatewayRuntime || row.OrganizationID != cached.OrganizationID || row.Provider != cached.Provider || row.IdentityRequired != cached.IdentityRequired {
		return ErrIngressChanged
	}
	if !row.DnsName.Valid {
		return ErrIngressChanged
	}
	host, err := requestorigin.CanonicalHost(row.DnsName.String)
	if err != nil || host != cached.DNSName {
		return ErrIngressChanged
	}
	return nil
}

func ingressFromRow(row repo.GetActiveIngressesByDNSNameRow) (Ingress, error) {
	if row.ID == uuid.Nil || row.OrganizationID == "" || row.Provider == "" {
		return Ingress{}, fmt.Errorf("%w: ingress authority is incomplete", ErrIngressUnavailable)
	}
	if !row.DnsName.Valid {
		return Ingress{}, fmt.Errorf("%w: ingress is not online", ErrIngressUnavailable)
	}
	host, err := requestorigin.CanonicalHost(row.DnsName.String)
	if err != nil {
		return Ingress{}, fmt.Errorf("%w: invalid ingress DNS name", ErrIngressUnavailable)
	}
	return Ingress{
		ID:               row.ID,
		OrganizationID:   row.OrganizationID,
		Provider:         row.Provider,
		DNSName:          host,
		IdentityRequired: row.IdentityRequired,
	}, nil
}
