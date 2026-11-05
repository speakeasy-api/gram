// Package tailnetgateway hosts private network ingresses on customer tailnets
// with tsnet. Each enabled ingress is served by one tailnet node per gateway
// pod; the pod's StatefulSet ordinal is the node slot, so in normal operation
// Kubernetes runs a single holder per node identity. There is no lease or
// fencing beyond that: a force-deleted or partitioned pod can briefly overlap
// with its replacement, and Tailscale's refusal to keep two sessions on one
// node key is what bounds the damage. Node state lives in Postgres so a
// restarted pod reconnects as the same device instead of registering a new one.
package tailnetgateway
