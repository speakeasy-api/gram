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

var (
	ErrIngressUnavailable = errors.New("private network ingress unavailable")
	ErrIngressChanged     = errors.New("private network ingress changed")
)

type Ingress struct {
	ID                     uuid.UUID
	OrganizationID         string
	Provider               string
	DNSName                string
	IdentityRequired       bool
	AttestorNamespace      string
	AttestorServiceAccount string
}

type ingressRepository interface {
	GetActiveIngressByAttestor(context.Context, repo.GetActiveIngressByAttestorParams) (repo.GetActiveIngressByAttestorRow, error)
	GetIngressServingState(context.Context, uuid.UUID) (repo.GetIngressServingStateRow, error)
}

type IngressLookup struct {
	repo ingressRepository
}

func NewIngressLookup(db repo.DBTX) *IngressLookup {
	return &IngressLookup{repo: repo.New(db)}
}

func (l *IngressLookup) ByAttestor(ctx context.Context, namespace, serviceAccount string) (Ingress, error) {
	if namespace == "" || serviceAccount == "" {
		return Ingress{}, fmt.Errorf("%w: attestor identity is incomplete", ErrIngressUnavailable)
	}
	row, err := l.repo.GetActiveIngressByAttestor(ctx, repo.GetActiveIngressByAttestorParams{
		AttestorNamespace:      namespace,
		AttestorServiceAccount: serviceAccount,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Ingress{}, ErrIngressUnavailable
	}
	if err != nil {
		return Ingress{}, fmt.Errorf("lookup private network ingress: %w", err)
	}
	return ingressFromRow(row, namespace, serviceAccount)
}

// Recheck compares every field that can alter serving authority. This does not
// rely on writers remembering to update a timestamp when changing policy.
func (l *IngressLookup) Recheck(ctx context.Context, cached Ingress) error {
	row, err := l.repo.GetIngressServingState(ctx, cached.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIngressUnavailable
	}
	if err != nil {
		return fmt.Errorf("recheck private network ingress: %w", err)
	}
	if !row.Enabled || row.Deleted || row.OrganizationID != cached.OrganizationID || row.Provider != cached.Provider || row.IdentityRequired != cached.IdentityRequired || row.AttestorNamespace != cached.AttestorNamespace || row.AttestorServiceAccount != cached.AttestorServiceAccount {
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

func ingressFromRow(row repo.GetActiveIngressByAttestorRow, namespace, serviceAccount string) (Ingress, error) {
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
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		DNSName:                host,
		IdentityRequired:       row.IdentityRequired,
		AttestorNamespace:      namespace,
		AttestorServiceAccount: serviceAccount,
	}, nil
}
