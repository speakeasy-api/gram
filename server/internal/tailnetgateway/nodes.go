package tailnetgateway

import (
	"fmt"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// NodeStatus is the heartbeat state of one (ingress, slot) node as written by
// the gateway pod that hosts it.
type NodeStatus string

const (
	// NodeStatusPending means the slot exists but no pod has hosted it yet.
	NodeStatusPending NodeStatus = "pending"

	// NodeStatusStarting means a pod is bringing the node up.
	NodeStatusStarting NodeStatus = "starting"

	// NodeStatusOnline means the node is connected and advertising its service.
	NodeStatusOnline NodeStatus = "online"

	// NodeStatusDraining means the hosting pod is shutting down.
	NodeStatusDraining NodeStatus = "draining"

	// NodeStatusError means the last start attempt failed, or a running node
	// failed its status check and was stopped. Either way the slot has no
	// node and is retried on a later poll, with backoff after repeated
	// failures.
	NodeStatusError NodeStatus = "error"

	// NodeStatusStopped means the hosting pod stopped the node cleanly.
	NodeStatusStopped NodeStatus = "stopped"

	// NodeStatusNeedsLogin means the slot's persisted identity no longer logs
	// in, typically because the device was removed from the tailnet. The pod
	// stops hosting it until the reconciler deletes the old device, resets
	// the slot and mints a fresh key.
	NodeStatusNeedsLogin NodeStatus = "needs_login"

	// NodeStatusAwaitingApproval means the node is connected but the tailnet
	// has not approved it to host the service, so clients cannot reach it.
	// The customer fixes it in their policy or admin console; the pod keeps
	// the node up and picks up approval on its next heartbeat.
	NodeStatusAwaitingApproval NodeStatus = "awaiting_approval"

	// NodeStatusAwaitingCertificate means the node is approved but Let's
	// Encrypt has not issued the service's TLS certificate yet, so every
	// client handshake fails. Usually the tailnet has used its weekly
	// allowance of new certificates; the pod retries on its own.
	NodeStatusAwaitingCertificate NodeStatus = "awaiting_certificate"
)

// ParseNodeStatus validates a stored status value.
func ParseNodeStatus(value string) (NodeStatus, error) {
	switch NodeStatus(value) {
	case NodeStatusPending, NodeStatusStarting, NodeStatusOnline, NodeStatusDraining, NodeStatusError, NodeStatusStopped, NodeStatusNeedsLogin, NodeStatusAwaitingApproval, NodeStatusAwaitingCertificate:
		return NodeStatus(value), nil
	default:
		return "", fmt.Errorf("invalid network ingress node status %q", value)
	}
}

const (
	// DefaultTag is the ACL tag every gateway node advertises. Customers own it
	// in their policy file and scope the OAuth client to it.
	DefaultTag = "tag:speakeasy"
)

// ValidateTag accepts a Tailscale ACL tag such as tag:speakeasy.
func ValidateTag(value string) error {
	tag, ok := strings.CutPrefix(value, "tag:")
	if !ok || tag == "" || len(k8svalidation.IsDNS1123Label(tag)) > 0 {
		return fmt.Errorf("invalid Tailscale tag %q", value)
	}
	return nil
}

// ValidateLabel accepts a lowercase DNS label usable as a service name.
func ValidateLabel(value string) error {
	if value == "" || value != strings.ToLower(value) || len(k8svalidation.IsDNS1123Label(value)) > 0 {
		return fmt.Errorf("invalid service label %q", value)
	}
	return nil
}
