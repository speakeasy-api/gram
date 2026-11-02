package netingress

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/netingress/repo"
)

type fakeIngressRepository struct {
	attestorParams repo.GetActiveIngressByAttestorParams
	attestorRow    repo.GetActiveIngressByAttestorRow
	attestorErr    error
	versionParams  repo.GetIngressServingStateParams
	versionRow     repo.GetIngressServingStateRow
	versionErr     error
	dnsName        string
	dnsRows        []repo.GetActiveIngressesByDNSNameRow
	dnsErr         error
}

func (f *fakeIngressRepository) GetActiveIngressesByDNSName(_ context.Context, dnsName string) ([]repo.GetActiveIngressesByDNSNameRow, error) {
	f.dnsName = dnsName
	return f.dnsRows, f.dnsErr
}

func (f *fakeIngressRepository) GetActiveIngressByAttestor(_ context.Context, params repo.GetActiveIngressByAttestorParams) (repo.GetActiveIngressByAttestorRow, error) {
	f.attestorParams = params
	return f.attestorRow, f.attestorErr
}

func (f *fakeIngressRepository) GetIngressServingState(_ context.Context, params repo.GetIngressServingStateParams) (repo.GetIngressServingStateRow, error) {
	f.versionParams = params
	return f.versionRow, f.versionErr
}

func TestIngressLookupByAttestor(t *testing.T) {
	t.Parallel()

	ingressID := uuid.New()
	fake := &fakeIngressRepository{attestorRow: repo.GetActiveIngressByAttestorRow{
		ID: ingressID, OrganizationID: "org_123", Provider: ProviderTailscale,
		DnsName: pgtype.Text{String: "Private.Example.ts.net", Valid: true}, IdentityRequired: true,
	}}
	lookup := &IngressLookup{repo: fake}

	ingress, err := lookup.ByAttestor(t.Context(), "attestor-ns", "attestor-sa")
	require.NoError(t, err)
	require.Equal(t, repo.GetActiveIngressByAttestorParams{AttestorNamespace: "attestor-ns", AttestorServiceAccount: "attestor-sa"}, fake.attestorParams)
	require.Equal(t, Ingress{
		ID: ingressID, OrganizationID: "org_123", Provider: ProviderTailscale,
		DNSName: "private.example.ts.net", IdentityRequired: true,
		AttestorNamespace: "attestor-ns", AttestorServiceAccount: "attestor-sa",
	}, ingress)
}

func TestIngressLookupByAttestorFailsClosed(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		namespace string
		account   string
		row       repo.GetActiveIngressByAttestorRow
		err       error
	}{
		{name: "missing namespace", account: "sa"},
		{name: "missing service account", namespace: "ns"},
		{name: "not found", namespace: "ns", account: "sa", err: pgx.ErrNoRows},
		{
			name: "offline without DNS", namespace: "ns", account: "sa",
			row: repo.GetActiveIngressByAttestorRow{ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale},
		},
		{
			name: "malformed DNS", namespace: "ns", account: "sa",
			row: repo.GetActiveIngressByAttestorRow{
				ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale,
				DnsName: pgtype.Text{String: "private.example.ts.net.", Valid: true},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookup := &IngressLookup{repo: &fakeIngressRepository{attestorRow: test.row, attestorErr: test.err}}
			_, err := lookup.ByAttestor(t.Context(), test.namespace, test.account)
			require.ErrorIs(t, err, ErrIngressUnavailable)
		})
	}
}

func TestIngressLookupRecheck(t *testing.T) {
	t.Parallel()

	ingressID := uuid.New()
	cached := Ingress{
		ID: ingressID, OrganizationID: "org_123", Provider: ProviderTailscale,
		DNSName: "private.example.ts.net", IdentityRequired: true,
		AttestorNamespace: "attestor-ns", AttestorServiceAccount: "attestor-sa",
	}
	unchanged := repo.GetIngressServingStateRow{
		OrganizationID: "org_123", Provider: ProviderTailscale,
		DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true}, IdentityRequired: true,
		AttestorNamespace: "attestor-ns", AttestorServiceAccount: "attestor-sa", Enabled: true,
	}

	for _, test := range []struct {
		name string
		row  repo.GetIngressServingStateRow
		err  error
		want error
	}{
		{name: "unchanged", row: unchanged},
		{name: "missing", err: pgx.ErrNoRows, want: ErrIngressUnavailable},
		{name: "disabled", row: func() repo.GetIngressServingStateRow { row := unchanged; row.Enabled = false; return row }(), want: ErrIngressChanged},
		{name: "deleted", row: func() repo.GetIngressServingStateRow { row := unchanged; row.Deleted = true; return row }(), want: ErrIngressChanged},
		{name: "identity policy changed", row: func() repo.GetIngressServingStateRow { row := unchanged; row.IdentityRequired = false; return row }(), want: ErrIngressChanged},
		{name: "DNS changed", row: func() repo.GetIngressServingStateRow {
			row := unchanged
			row.DnsName.String = "other.example.ts.net"
			return row
		}(), want: ErrIngressChanged},
		{name: "organization changed", row: func() repo.GetIngressServingStateRow { row := unchanged; row.OrganizationID = "org_other"; return row }(), want: ErrIngressChanged},
		{name: "attestor rebound", row: func() repo.GetIngressServingStateRow {
			row := unchanged
			row.AttestorServiceAccount = "other-sa"
			return row
		}(), want: ErrIngressChanged},
		{name: "lookup failure", err: errors.New("database unavailable"), want: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeIngressRepository{versionRow: test.row, versionErr: test.err}
			lookup := &IngressLookup{repo: fake}
			err := lookup.Recheck(t.Context(), cached)
			require.Equal(t, repo.GetIngressServingStateParams{ID: ingressID, OrganizationID: "org_123"}, fake.versionParams)
			if test.want == nil {
				require.NoError(t, err)
				return
			}
			if errors.Is(test.want, ErrIngressUnavailable) || errors.Is(test.want, ErrIngressChanged) {
				require.ErrorIs(t, err, test.want)
			} else {
				require.ErrorContains(t, err, test.want.Error())
			}
		})
	}
}

func TestIngressLookupByDNSName(t *testing.T) {
	t.Parallel()

	ingressID := uuid.New()
	row := repo.GetActiveIngressesByDNSNameRow{
		ID: ingressID, OrganizationID: "org_123", Provider: ProviderTailscale,
		DnsName: pgtype.Text{String: "speakeasy.example.ts.net", Valid: true}, IdentityRequired: true,
		AttestorNamespace: "ns-derived", AttestorServiceAccount: "sa-derived",
	}
	fake := &fakeIngressRepository{dnsRows: []repo.GetActiveIngressesByDNSNameRow{row}}
	lookup := &IngressLookup{repo: fake}

	ingress, err := lookup.ByDNSName(t.Context(), "Speakeasy.Example.ts.net")
	require.NoError(t, err)
	require.Equal(t, "speakeasy.example.ts.net", fake.dnsName, "host is canonicalised before lookup")
	require.Equal(t, Ingress{
		ID: ingressID, OrganizationID: "org_123", Provider: ProviderTailscale,
		DNSName: "speakeasy.example.ts.net", IdentityRequired: true,
		AttestorNamespace: "ns-derived", AttestorServiceAccount: "sa-derived",
	}, ingress)
}

func TestIngressLookupByDNSNameFailsClosed(t *testing.T) {
	t.Parallel()

	valid := repo.GetActiveIngressesByDNSNameRow{
		ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale,
		DnsName: pgtype.Text{String: "speakeasy.example.ts.net", Valid: true}, IdentityRequired: false,
		AttestorNamespace: "ns", AttestorServiceAccount: "sa",
	}
	for _, test := range []struct {
		name string
		host string
		rows []repo.GetActiveIngressesByDNSNameRow
		err  error
	}{
		{name: "empty host", host: "", rows: []repo.GetActiveIngressesByDNSNameRow{valid}},
		{name: "invalid host", host: "bad host", rows: []repo.GetActiveIngressesByDNSNameRow{valid}},
		{name: "not found", host: "speakeasy.example.ts.net"},
		{name: "ambiguous", host: "speakeasy.example.ts.net", rows: []repo.GetActiveIngressesByDNSNameRow{valid, valid}},
		{name: "database error", host: "speakeasy.example.ts.net", err: errors.New("boom")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookup := &IngressLookup{repo: &fakeIngressRepository{dnsRows: test.rows, dnsErr: test.err}}
			_, err := lookup.ByDNSName(t.Context(), test.host)
			require.Error(t, err)
			if test.err == nil {
				require.ErrorIs(t, err, ErrIngressUnavailable)
			}
		})
	}
}
