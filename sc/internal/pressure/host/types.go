// Package host ports the tailnet gateway's host and slot lifecycle (PR #8377,
// server/internal/tailnetgateway/gateway.go) onto the sc statechart package.
//
// It is a pressure test of sc, not production code. The gateway loop is kept
// as it was: every poll lists the ingresses this pod must host, stops the
// slots that are no longer wanted, starts idle ones within the host cap, and
// probes every running node. What changed is the slot: its lifecycle is the
// chart in chart.go, one sc.Instance per slot driven from the poll loop,
// instead of a struct of flags and the probeTransition function. The
// original's move is the chart action every recording state runs on entry.
//
// Everything the original read from Postgres, Tailscale or OpenTelemetry is
// behind the narrow interfaces in this file.
package host

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// NodeStatus is the heartbeat state of one (ingress, slot) node as written by
// the gateway pod that hosts it. The values are the ones the reconciler and
// the dashboard read from network_ingress_nodes.status.
type NodeStatus string

const (
	// NodeStatusPending means the slot exists but no pod has hosted it yet.
	NodeStatusPending NodeStatus = "pending"

	// NodeStatusStarting means a pod is bringing the node up.
	NodeStatusStarting NodeStatus = "starting"

	// NodeStatusOnline means the node is connected, approved and certified.
	NodeStatusOnline NodeStatus = "online"

	// NodeStatusDraining means the hosting pod is stopping the node.
	NodeStatusDraining NodeStatus = "draining"

	// NodeStatusError means the last start failed or a running node failed.
	NodeStatusError NodeStatus = "error"

	// NodeStatusStopped means the hosting pod stopped the node cleanly.
	NodeStatusStopped NodeStatus = "stopped"

	// NodeStatusNeedsLogin means control refused the slot's identity.
	NodeStatusNeedsLogin NodeStatus = "needs_login"

	// NodeStatusAwaitingApproval means the tailnet has not approved the node
	// to host the service.
	NodeStatusAwaitingApproval NodeStatus = "awaiting_approval"

	// NodeStatusAwaitingCertificate means the node is approved but holds no
	// TLS certificate for the service yet.
	NodeStatusAwaitingCertificate NodeStatus = "awaiting_certificate"
)

var (
	// ErrNodeNeedsLogin means control refused the node's persisted identity.
	ErrNodeNeedsLogin = errors.New("tailnet node needs login")

	// ErrEncryptionKeyUnavailable stands in for orgencryption's suspended or
	// unreachable key errors.
	ErrEncryptionKeyUnavailable = errors.New("organization encryption key unavailable")

	// ErrSlotNotFound is returned by [Store.GetSlot] for a missing slot row.
	ErrSlotNotFound = errors.New("tailnet node slot not found")
)

// NodeHostname is the tailnet hostname of the node hosting one slot.
func NodeHostname(slot int32) string {
	return fmt.Sprintf("speakeasy-gw-%d", slot)
}

// ServiceName is the Tailscale Service advertised for one ingress label.
func ServiceName(label string) string {
	return "svc:" + label
}

// Ingress is one enabled ingress this pod must host.
type Ingress struct {
	ID               uuid.UUID `json:"id"`
	OrganizationID   string    `json:"organization_id"`
	OrganizationSlug string    `json:"organization_slug"`
	Label            string    `json:"label"`
}

// NodeSpec describes one tailnet node to start.
type NodeSpec struct {
	IngressID      uuid.UUID
	OrganizationID string
	Slot           int32
	Hostname       string
	ServiceName    string
	Tag            string
	// AuthKey registers the node on first start; it is empty once state exists.
	AuthKey      string
	Store        NodeStateStore
	StateDir     string
	ControlURL   string
	StartTimeout time.Duration
	// CertificateEvents is told the outcome of every certificate request.
	CertificateEvents func(ctx context.Context, operation, result string)
}

// NodeState is a point-in-time view of a running node.
type NodeState struct {
	Online               bool
	BackendState         string
	NeedsLogin           bool
	AwaitingApproval     bool
	AwaitingCertificate  bool
	CertificateStatus    string
	CertificateExpiresAt time.Time
	NodeID               string
	FQDN                 string
}

// Node is a running tailnet node hosting one ingress. Implementations must be
// comparable (pointer types are), because the chart matches a probe to the
// node it was taken from.
type Node interface {
	Serve(handler http.Handler) error
	Status(ctx context.Context) (NodeState, error)
	Close(ctx context.Context) error
}

// Runtime starts nodes. The production runtime embeds tsnet.
type Runtime interface {
	Start(ctx context.Context, spec NodeSpec) (Node, error)
}

// NodeStateStore is the slice of ipn.StateStore plus PostgresStateStore the
// lifecycle uses.
type NodeStateStore interface {
	ReadState(key string) ([]byte, error)
	WriteState(key string, value []byte) error
	// Registered reports whether a login completed (a profile list exists).
	Registered(ctx context.Context) (bool, error)
	// Reset deletes every persisted key of the slot.
	Reset(ctx context.Context) error
}

// SlotRow is the part of a network_ingress_nodes row a start reads.
type SlotRow struct {
	Status           string
	AuthKeyEncrypted string
	HasAuthKey       bool
	// AuthKeyExpiresAt is zero when no expiry is recorded.
	AuthKeyExpiresAt time.Time
	Retired          bool
}

// Heartbeat is one HeartbeatNetworkIngressNode call.
type Heartbeat struct {
	IngressID      uuid.UUID
	OrganizationID string
	Slot           int32
	PodName        string
	Status         NodeStatus
	NodeID         string
	DNSName        string
	LastError      string
}

// Store is what the gateway reads and writes in Postgres, including the
// reconcile outbox and the organization-encrypted secrets.
type Store interface {
	// ListDesired lists enabled gateway ingresses on shard whose slot row
	// for slot exists and is not retired.
	ListDesired(ctx context.Context, slot int32, shard string) ([]Ingress, error)
	// GetSlot returns ErrSlotNotFound when the row is missing.
	GetSlot(ctx context.Context, ingressID uuid.UUID, organizationID string, slot int32) (SlotRow, error)
	ClearAuthKey(ctx context.Context, ingressID uuid.UUID, organizationID string, slot int32) error
	// Heartbeat returns the number of rows written; a retired row only
	// accepts draining and stopped beats.
	Heartbeat(ctx context.Context, beat Heartbeat) (int64, error)
	// EnqueueReconcile adds a reconcile request for the ingress to the
	// outbox in its own transaction.
	EnqueueReconcile(ctx context.Context, organizationID string, ingressID uuid.UUID) error
	NodeStateStore(ingressID uuid.UUID, organizationID string, slot int32) (NodeStateStore, error)
	DecryptAuthKey(ctx context.Context, organizationID, ciphertext string) (string, error)
}

// Operation and Result label the lifecycle metric.
type (
	Operation string
	Result    string
)

const (
	OperationStart     Operation = "start"
	OperationStop      Operation = "stop"
	OperationHeartbeat Operation = "heartbeat"

	ResultOK    Result = "ok"
	ResultError Result = "error"
)

// CertificateObservation is one running node's certificate expiry.
type CertificateObservation struct {
	OrganizationSlug string
	IngressID        uuid.UUID
	Slot             int32
	ExpiresAt        time.Time
}

// Metrics is the gateway's instrument set (metrics.go in the original).
type Metrics interface {
	HostStatusChanged(ctx context.Context, orgSlug string, slot int32, previous, next NodeStatus)
	Lifecycle(ctx context.Context, orgSlug string, operation Operation, result Result)
	StartDuration(ctx context.Context, orgSlug string, firstRegistration bool, duration time.Duration)
	Poll(ctx context.Context, duration time.Duration)
	CertificateRequest(ctx context.Context, orgSlug, operation, result string)
	// ObserveCertificates reports list at every collection until the
	// returned function is called.
	ObserveCertificates(list func() []CertificateObservation) (unregister func(), err error)
}
