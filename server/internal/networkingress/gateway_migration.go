package networkingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// RuntimeOperator names the Tailscale Kubernetes operator in audit
	// records and staff views. In the database it is an unset runtime.
	RuntimeOperator = "operator"

	// credentialCheckTTL bounds the throwaway key minted to prove the saved
	// OAuth client can register gateway nodes. It is revoked immediately; the
	// TTL only limits the damage if revocation fails.
	credentialCheckTTL = time.Minute
)

var (
	// ErrGatewayIngressNotFound means the organization has no live ingress.
	ErrGatewayIngressNotFound = errors.New("organization has no private network ingress")

	// ErrGatewayCredentialsRejected means the saved OAuth client cannot mint
	// keys for the gateway tag, so moving would leave the ingress offline.
	ErrGatewayCredentialsRejected = errors.New("saved Tailscale credentials cannot register tailnet gateway nodes")

	// ErrGatewayCredentialsMissing means no usable credentials are saved.
	ErrGatewayCredentialsMissing = errors.New("organization has no usable Tailscale credentials saved")
)

// GatewayNode is one gateway slot as reported by the pod hosting it.
type GatewayNode struct {
	// Slot is the node identity index, equal to the hosting pod's ordinal.
	Slot int32

	// Status is the latest heartbeat status.
	Status string

	// PodName is the pod that last reported the slot.
	PodName string

	// LastError is the latest reported failure, if any.
	LastError string

	// LastSeenAt is when the slot last heartbeated; zero if never.
	LastSeenAt time.Time

	// ConnectedSince is when the slot came online; zero if offline.
	ConnectedSince time.Time

	// Retired is true when the slot is being drained.
	Retired bool
}

// GatewayIngressState is a staff view of one organization's ingress and
// which runtime hosts it.
type GatewayIngressState struct {
	// Ingress is the organization's live ingress row.
	Ingress repo.NetworkIngress

	// Runtime is RuntimeGateway or RuntimeOperator.
	Runtime string

	// Nodes are the gateway slots; empty while the operator hosts it.
	Nodes []GatewayNode
}

// GatewayMigrator moves operator-hosted ingresses onto the tailnet gateway
// one organization at a time. It is driven by staff, not by customers.
type GatewayMigrator struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	enc       *encryption.Client
	api       tailscaleapi.Client
	tag       string
	requester ReconcileRequester
	audit     *audit.Logger
}

// NewGatewayMigrator wires the migrator. tag is the ACL tag gateway nodes
// advertise; the saved OAuth client must be able to mint keys for it.
func NewGatewayMigrator(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, api tailscaleapi.Client, tag string, requester ReconcileRequester, auditLogger *audit.Logger) (*GatewayMigrator, error) {
	if db == nil || enc == nil || api == nil || requester == nil || auditLogger == nil {
		return nil, errors.New("gateway migrator requires a database, encryption client, Tailscale API client, reconcile requester and audit logger")
	}
	if tag == "" {
		return nil, errors.New("gateway migrator requires the gateway tag")
	}
	return &GatewayMigrator{logger: logger, db: db, enc: enc, api: api, tag: tag, requester: requester, audit: auditLogger}, nil
}

// Tag is the ACL tag the saved OAuth client must own before a move.
func (m *GatewayMigrator) Tag() string {
	return m.tag
}

// State reports the organization's ingress and runtime. It returns
// ErrGatewayIngressNotFound when there is no live ingress.
func (m *GatewayMigrator) State(ctx context.Context, organizationID string) (GatewayIngressState, error) {
	ingress, err := repo.New(m.db).GetNetworkIngressByOrganization(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GatewayIngressState{}, ErrGatewayIngressNotFound
	}
	if err != nil {
		return GatewayIngressState{}, fmt.Errorf("load network ingress: %w", err)
	}
	return m.state(ctx, m.db, ingress)
}

func (m *GatewayMigrator) state(ctx context.Context, db repo.DBTX, ingress repo.NetworkIngress) (GatewayIngressState, error) {
	rows, err := repo.New(db).ListNetworkIngressGatewayNodes(ctx, repo.ListNetworkIngressGatewayNodesParams{
		NetworkIngressID: ingress.ID, OrganizationID: ingress.OrganizationID,
	})
	if err != nil {
		return GatewayIngressState{}, fmt.Errorf("list gateway nodes: %w", err)
	}
	nodes := make([]GatewayNode, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, GatewayNode{
			Slot: row.Slot, Status: row.Status, PodName: row.PodName.String, LastError: row.LastError.String,
			LastSeenAt: row.LastSeenAt.Time, ConnectedSince: row.ConnectedSince.Time, Retired: row.RetiredAt.Valid,
		})
	}
	return GatewayIngressState{Ingress: ingress, Runtime: runtimeName(ingress), Nodes: nodes}, nil
}

// MoveToGateway proves the saved OAuth client can register gateway nodes,
// then records the gateway runtime and queues a reconcile, which tears down
// the operator resources and brings the gateway up. Moving an ingress that is
// already on the gateway returns its state unchanged.
func (m *GatewayMigrator) MoveToGateway(ctx context.Context, organizationID string, actor urn.Principal, actorDisplayName *string) (GatewayIngressState, error) {
	current, err := m.State(ctx, organizationID)
	if err != nil {
		return GatewayIngressState{}, err
	}
	if current.Runtime == RuntimeGateway {
		return current, nil
	}
	if err := m.checkCredentials(ctx, current.Ingress); err != nil {
		return GatewayIngressState{}, err
	}

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return GatewayIngressState{}, fmt.Errorf("begin gateway move: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, organizationID); err != nil {
		return GatewayIngressState{}, fmt.Errorf("lock network ingress lifecycle: %w", err)
	}
	moved, err := queries.MoveNetworkIngressToGateway(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted or moved by someone else since the check: report what is there now.
		if err := tx.Rollback(ctx); err != nil {
			return GatewayIngressState{}, fmt.Errorf("release gateway move: %w", err)
		}
		return m.State(ctx, organizationID)
	}
	if err != nil {
		return GatewayIngressState{}, fmt.Errorf("move network ingress to gateway: %w", err)
	}
	if moved.ID != current.Ingress.ID {
		return GatewayIngressState{}, errors.New("network ingress changed during the gateway move; retry")
	}
	if err := m.audit.LogNetworkIngressMoveRuntime(ctx, tx, audit.LogNetworkIngressMoveRuntimeEvent{
		NetworkIngressEventBase: audit.NetworkIngressEventBase{
			OrganizationID: organizationID, Actor: actor, ActorDisplayName: actorDisplayName,
			NetworkIngressURN: urn.NewNetworkIngress(moved.ID), Hostname: moved.Hostname,
		},
		From: current.Runtime,
		To:   RuntimeGateway,
	}); err != nil {
		return GatewayIngressState{}, fmt.Errorf("record gateway move: %w", err)
	}
	if err := m.requester.Enqueue(ctx, tx, organizationID, moved.ID); err != nil {
		return GatewayIngressState{}, fmt.Errorf("queue gateway reconcile: %w", err)
	}
	state, err := m.state(ctx, tx, moved)
	if err != nil {
		return GatewayIngressState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayIngressState{}, fmt.Errorf("commit gateway move: %w", err)
	}
	return state, nil
}

// checkCredentials mints and immediately revokes a gateway-tagged key. It is
// the only way to prove the OAuth client owns the tag before the operator
// backend is torn down.
func (m *GatewayMigrator) checkCredentials(ctx context.Context, ingress repo.NetworkIngress) error {
	if !ingress.CredentialsEncrypted.Valid || ingress.CredentialsEncrypted.String == "" {
		return ErrGatewayCredentialsMissing
	}
	plaintext, err := m.enc.Decrypt(ingress.CredentialsEncrypted.String)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGatewayCredentialsMissing, err)
	}
	parsed, err := k8s.ParseTailscaleCredentials([]byte(plaintext))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGatewayCredentialsMissing, err)
	}
	creds := tailscaleapi.Credentials{ClientID: parsed.ClientID, ClientSecret: parsed.ClientSecret}

	key, err := m.api.CreateAuthKey(ctx, creds, tailscaleapi.AuthKeyRequest{
		Tag: m.tag, Description: "Speakeasy tailnet gateway credential check", TTL: credentialCheckTTL,
	})
	if errors.Is(err, tailscaleapi.ErrUnauthorized) {
		return fmt.Errorf("%w: %w", ErrGatewayCredentialsRejected, err)
	}
	if err != nil {
		return fmt.Errorf("check Tailscale credentials: %w", err)
	}
	if err := m.api.DeleteAuthKey(ctx, creds, key.ID); err != nil {
		// The key is single-use and expires within a minute, so this is not
		// worth failing the move for.
		m.logger.WarnContext(ctx, "revoke Tailscale credential check key", attr.SlogError(err))
	}
	return nil
}

func runtimeName(ingress repo.NetworkIngress) string {
	if ingress.Runtime.String == RuntimeGateway {
		return RuntimeGateway
	}
	return RuntimeOperator
}
