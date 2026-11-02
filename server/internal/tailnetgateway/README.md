# Tailnet gateway

`gram tailnet-gateway` hosts every enabled private network ingress on its
customer's tailnet using [tsnet](https://tailscale.com/kb/1244/tsnet). It
replaces the Tailscale Kubernetes operator, its privileged proxy pods and the
per-ingress attestor pods.

## How it works

- The gateway runs as a StatefulSet. The pod ordinal is the **slot**: pod `n`
  is tailnet node `speakeasy-gw-<n>` for every customer. Kubernetes guarantees
  one live pod per ordinal, so no lease is needed.
- The reconciler (`gram network-ingress-worker`) creates one
  `network_ingress_nodes` row per slot, defines the Tailscale Service
  `svc:<label>` through the Tailscale API and mints a one-shot, preauthorized,
  tagged auth key for every slot that has no persisted state. The customer's
  OAuth client never leaves the control plane.
- Every 15s each pod lists enabled ingresses for its shard. For each it starts
  a `tsnet.Server` whose `ipn.StateStore` is backed by
  `network_ingress_node_states` (encrypted with the application key). Restarts
  reconnect as the same device, so customers never see registration churn.
- The node advertises the service with `ListenService`. Requests arrive over
  tsnet's loopback proxy; the gateway resolves the peer with `WhoIs`, replaces
  every `Tailscale-*` header with the resolved identity and forwards to the
  private listener with the pod's projected service-account token, exactly as
  the attestor did.
- Each pod heartbeats its slot rows; the reconciler reports the ingress online
  when any slot has a fresh `online` heartbeat.

## Running locally

The daemon needs a real tailnet, so it is opt-in. Provision an ingress through
the dashboard with the `tag:speakeasy` policy, then set in `mise.local.toml`:

```toml
GRAM_TAILNET_GATEWAY_UPSTREAM_URL = "https://localhost:<private-listener-port>"
GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE = "<path to the listener CA>"
GRAM_TAILNET_GATEWAY_TOKEN_PATH = "<path to a file holding the attestation token>"
```

and start it with `pitchfork start tailnet-gateway`. Readiness is served on
`http://localhost:$GRAM_TAILNET_GATEWAY_CONTROL_PORT/readyz`.

The pod needs outbound HTTPS to `controlplane.tailscale.com`,
`login.tailscale.com` and the DERP relays (`*.tailscale.com:443`). UDP 41641
egress is optional; without it the node relays through DERP.

## Verifying end to end

1. From a device on the tailnet, `curl https://<label>.<tailnet>.ts.net/mcp/<slug>`.
2. Confirm the private listener logs the Tailscale identity and that
   `identity_required` rejects a tagged device.
3. Restart the daemon and confirm the admin console shows the same device, not
   a new registration.
4. Run a second instance with `GRAM_TAILNET_GATEWAY_ORDINAL=1` and confirm both
   nodes host the service.
