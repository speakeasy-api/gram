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

	// NodeStatusError means the last start attempt failed.
	NodeStatusError NodeStatus = "error"

	// NodeStatusStopped means the hosting pod stopped the node cleanly.
	NodeStatusStopped NodeStatus = "stopped"
)

// ParseNodeStatus validates a stored status value.
func ParseNodeStatus(value string) (NodeStatus, error) {
	switch NodeStatus(value) {
	case NodeStatusPending, NodeStatusStarting, NodeStatusOnline, NodeStatusDraining, NodeStatusError, NodeStatusStopped:
		return NodeStatus(value), nil
	default:
		return "", fmt.Errorf("invalid network ingress node status %q", value)
	}
}

const (
	// DefaultTag is the ACL tag every gateway node advertises. Customers own it
	// in their policy file and scope the OAuth client to it.
	DefaultTag = "tag:speakeasy"

	// nodeHostnamePrefix precedes the slot number in a node's hostname.
	nodeHostnamePrefix = "speakeasy-gw-"
)

// NodeHostname is the tailnet hostname of the node hosting one slot. Node and
// service names share MagicDNS, so it is distinct from the service label.
func NodeHostname(slot int32) string {
	return fmt.Sprintf("%s%d", nodeHostnamePrefix, slot)
}

// ServiceName is the Tailscale Service advertised for one ingress label.
func ServiceName(label string) string {
	return "svc:" + label
}

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
