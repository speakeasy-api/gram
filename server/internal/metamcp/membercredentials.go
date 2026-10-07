package metamcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// lockGatewayIssuerForClientBinding takes the gateway issuer's owner-binding
// lock, shared with manual client attachment, consumer writers and issuer
// deletion. Take it before any remote issuer binding lock.
func lockGatewayIssuerForClientBinding(ctx context.Context, dbtx pgx.Tx, gatewayIssuerID uuid.UUID) error {
	if err := usersessionsrepo.New(dbtx).LockUserSessionIssuerForOwnerBinding(ctx, gatewayIssuerID); err != nil {
		return fmt.Errorf("lock gateway issuer for client binding: %w", err)
	}
	return nil
}

// lockGatewayIssuersForSwitch takes the owner-binding locks of a gateway's old
// and new issuers in ascending order, so two gateways swapping issuers cannot
// deadlock. Advisory locks are reentrant, so later per-issuer locks are no-ops.
func lockGatewayIssuersForSwitch(ctx context.Context, dbtx pgx.Tx, issuerIDs ...uuid.UUID) error {
	ids := slices.Clone(issuerIDs)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range slices.Compact(ids) {
		if err := lockGatewayIssuerForClientBinding(ctx, dbtx, id); err != nil {
			return err
		}
	}
	return nil
}

// releaseGatewayIssuer clears per-member credentials from an issuer the
// gateway no longer consumes, after it moved to another issuer or was
// deleted. Left in place they would make the issuer unusable for any other
// consumer. It is a no-op while anything still consumes the issuer. The
// caller holds the issuer's owner-binding lock, which every binding writer
// takes first; removing bindings cannot break the remote issuer invariants,
// so no remote issuer lock is needed.
func releaseGatewayIssuer(ctx context.Context, txRepo *repo.Queries, issuerID uuid.UUID) (int64, error) {
	rows, err := txRepo.DetachOrphanedGatewayMemberCredentials(ctx, issuerID)
	if err != nil {
		return 0, fmt.Errorf("detach orphaned gateway member credentials: %w", err)
	}
	return rows, nil
}

// wireMemberClients binds members' provider clients to the gateway's issuer so
// consent can offer them. It is idempotent, so a gateway save can re-run it to
// repair skipped bindings.
//
// The original rule binds at most one client per remote issuer, taken from
// the first member that has one. With gateway member credentials enabled and
// the gateway owning its issuer exclusively, each member's own client is bound
// instead, so members that share an authorization server keep separate
// credentials. The caller holds the gateway issuer's owner-binding lock;
// identities must be ordered by remote issuer so binding locks are taken in
// ascending order.
func wireMemberClients(
	ctx context.Context,
	dbtx pgx.Tx,
	txRepo *repo.Queries,
	projectID, gatewayIssuerID uuid.UUID,
	identities []repo.ListMemberProviderIdentitiesRow,
	memberCredentials bool,
) (int64, error) {
	perMember := false
	if memberCredentials {
		exclusive, err := remotesessions.GatewayOwnsIssuerExclusively(ctx, remotesessionsrepo.New(dbtx), gatewayIssuerID)
		if err != nil {
			return 0, fmt.Errorf("check gateway issuer ownership: %w", err)
		}
		perMember = exclusive
	}

	var attached int64
	for _, identity := range identities {
		if !identity.RemoteSessionIssuerID.Valid || !identity.UserSessionIssuerID.Valid {
			continue
		}
		if err := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, identity.RemoteSessionIssuerID.UUID); err != nil {
			return attached, fmt.Errorf("lock remote session issuer for client binding: %w", err)
		}

		var rows int64
		var err error
		if perMember {
			rows, err = txRepo.AutoAttachMemberOwnClient(ctx, repo.AutoAttachMemberOwnClientParams{
				GatewayIssuerID: gatewayIssuerID,
				ProjectID:       projectID,
				MemberIssuerID:  identity.UserSessionIssuerID.UUID,
				RemoteIssuerID:  identity.RemoteSessionIssuerID.UUID,
			})
		} else {
			rows, err = txRepo.AutoAttachMemberProviderClient(ctx, repo.AutoAttachMemberProviderClientParams{
				GatewayIssuerID: gatewayIssuerID,
				ProjectID:       projectID,
				MemberIssuerID:  identity.UserSessionIssuerID.UUID,
				RemoteIssuerID:  identity.RemoteSessionIssuerID.UUID,
			})
		}
		if err != nil {
			return attached, fmt.Errorf("attach member provider client: %w", err)
		}
		attached += rows
	}
	return attached, nil
}

// detachMemberClients unbinds a removed member's provider client from the
// gateway's issuer. With gateway member credentials enabled it first removes
// the member's own client unless a surviving consumer still needs that exact
// client, then re-wires surviving members so none is left without a binding.
// Either way the original provider-wide detach runs, which only fires once no
// consumer of the remote issuer remains. The member row must already be
// soft-deleted and the caller holds the gateway issuer's owner-binding lock.
func detachMemberClients(
	ctx context.Context,
	dbtx pgx.Tx,
	txRepo *repo.Queries,
	projectID, metaID, gatewayIssuerID uuid.UUID,
	removed repo.ListMemberProviderIdentitiesRow,
	memberCredentials bool,
) (int64, error) {
	if !removed.RemoteSessionIssuerID.Valid {
		return 0, nil
	}
	remoteIssuerID := removed.RemoteSessionIssuerID.UUID
	if err := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, remoteIssuerID); err != nil {
		return 0, fmt.Errorf("lock remote session issuer for client binding: %w", err)
	}

	var detached int64
	if memberCredentials && removed.UserSessionIssuerID.Valid {
		rows, err := txRepo.AutoDetachMemberOwnClient(ctx, repo.AutoDetachMemberOwnClientParams{
			GatewayIssuerID: gatewayIssuerID,
			MemberIssuerID:  removed.UserSessionIssuerID.UUID,
			RemoteIssuerID:  remoteIssuerID,
			ProjectID:       projectID,
		})
		if err != nil {
			return 0, fmt.Errorf("detach member own client: %w", err)
		}
		detached += rows
	}

	rows, err := txRepo.AutoDetachMemberProviderClient(ctx, repo.AutoDetachMemberProviderClientParams{
		GatewayIssuerID: gatewayIssuerID,
		RemoteIssuerID:  remoteIssuerID,
		ProjectID:       projectID,
	})
	if err != nil {
		return detached, fmt.Errorf("detach member provider client: %w", err)
	}
	detached += rows

	if memberCredentials && detached > 0 {
		identities, err := txRepo.ListMemberProviderIdentities(ctx, repo.ListMemberProviderIdentitiesParams{
			MetaMcpServerID: metaID,
			ProjectID:       projectID,
		})
		if err != nil {
			return detached, fmt.Errorf("list member provider identities: %w", err)
		}
		// Only members of the same remote issuer can have lost a binding, and
		// staying on that one issuer keeps binding locks in ascending order.
		identities = slices.DeleteFunc(identities, func(identity repo.ListMemberProviderIdentitiesRow) bool {
			return identity.RemoteSessionIssuerID.UUID != remoteIssuerID
		})
		if _, err := wireMemberClients(ctx, dbtx, txRepo, projectID, gatewayIssuerID, identities, memberCredentials); err != nil {
			return detached, err
		}
	}
	return detached, nil
}
