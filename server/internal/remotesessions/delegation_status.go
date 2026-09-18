package remotesessions

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// GetClientDelegationStatus only reads stored configuration and observations.
// It must not load a live provider, decrypt a credential, or perform discovery.
func (s *Service) GetClientDelegationStatus(ctx context.Context, payload *orgclientsgen.GetClientDelegationStatusPayload) (*orgclientsgen.OrganizationClientDelegationStatus, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id")
	}
	q := repo.New(s.db)
	row, err := q.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{ID: clientID, OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get delegation client")
	}
	// Delegation is organizational, never a project credential surface.
	if row.RemoteSessionClient.ProjectID.Valid || !row.RemoteSessionClient.OrganizationID.Valid || row.RemoteSessionClient.OrganizationID.String != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeNotFound)
	}
	issuer, err := q.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{ID: row.RemoteSessionClient.RemoteSessionIssuerID, OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID), IncludeGlobal: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get delegation issuer")
	}
	if issuer.ProjectID.Valid {
		return nil, oops.C(oops.CodeNotFound)
	}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	result := &orgclientsgen.OrganizationClientDelegationStatus{Status: "unknown", WindowStart: since.Format(time.RFC3339Nano), Observations: []*orgclientsgen.DelegationStatusCount{}}
	keyRevision, err := federatedSigningKeyRevision(ctx, s.db, authCtx.ActiveOrganizationID, row.RemoteSessionClient)
	if err != nil {
		if errors.Is(err, ErrFederatedConfiguration) {
			return result, nil
		}
		return nil, oops.E(oops.CodeUnexpected, err, "read delegation signing key revision")
	}
	hash := FederatedDelegationConfigurationHash(authCtx.ActiveOrganizationID, issuer, row.RemoteSessionClient, keyRevision)
	if hash == "" {
		return result, nil
	}
	counts, err := q.CountTrustedDelegationObservations(ctx, repo.CountTrustedDelegationObservationsParams{OrganizationID: authCtx.ActiveOrganizationID, ClientID: clientID, ConfigHash: hash, ObservedSince: pgtype.Timestamptz{Time: since, Valid: true}})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count delegation observations")
	}
	result.Observations = delegationStatusCounts(counts)
	if len(result.Observations) > 0 {
		result.Status = "observed"
	}
	return result, nil
}

func delegationStatusTimestamp(ts pgtype.Timestamptz) *string {
	if !ts.Valid || ts.InfinityModifier != pgtype.Finite {
		return nil
	}
	s := ts.Time.UTC().Format(time.RFC3339Nano)
	return &s
}

func delegationStatusCounts(rows []repo.CountTrustedDelegationObservationsRow) []*orgclientsgen.DelegationStatusCount {
	result := make([]*orgclientsgen.DelegationStatusCount, 0, len(rows))
	for _, row := range rows {
		// A closed allowlist prevents future/internal or provider-derived strings
		// from accidentally becoming an externally visible status.
		switch row.ObservationStatus {
		case "durable_credential_present", "assertion_only", "offline_unsupported", "offline_not_requested", "refused", "reauthentication_required", "temporary_failure", "configuration_failure":
		default:
			continue
		}
		if row.ObservationCount <= 0 {
			continue
		}
		result = append(result, &orgclientsgen.DelegationStatusCount{
			Status: row.ObservationStatus, Count: row.ObservationCount,
			LastObservedAt:           delegationStatusTimestamp(row.LastObservedAt),
			LastCredentialObtainedAt: delegationStatusTimestamp(row.LastCredentialObtainedAt),
			LastRefreshSucceededAt:   delegationStatusTimestamp(row.LastRefreshSucceededAt),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Status < result[j].Status })
	return result
}
