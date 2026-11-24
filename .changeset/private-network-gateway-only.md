---
"server": patch
---

Every private network connection is now created on and hosted by the Speakeasy tailnet gateway. The Tailscale Kubernetes operator provider, its per-connection attestor, and the `private-network-gateway` rollout check are removed. The private listener accepts only the gateway's service account. A connection row created for the operator is reported as `unsupported_provider` and can still be deleted. The reconciler no longer reads the operator settings (`GRAM_NETWORK_INGRESS_OPERATOR_*`, `GRAM_NETWORK_INGRESS_GATEWAY_ENABLED`, backend, attestor, tag, CIDR and token audience settings), so they can be removed from deployments.
