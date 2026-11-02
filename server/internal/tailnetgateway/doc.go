// Package tailnetgateway hosts private network ingresses on customer tailnets
// with tsnet. Each enabled ingress is served by one tailnet node per gateway
// pod; the pod's StatefulSet ordinal is the node slot, so Kubernetes guarantees
// a single live holder per node identity. Node state lives in Postgres so a
// restarted pod reconnects as the same device instead of registering a new one.
package tailnetgateway
