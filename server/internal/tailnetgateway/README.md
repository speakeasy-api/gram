# Tailnet gateway

`gram tailnet-gateway` hosts every enabled private network ingress on its
customer's tailnet using [tsnet](https://tailscale.com/kb/1244/tsnet). It
replaces the Tailscale Kubernetes operator, its privileged proxy pods and the
per-ingress attestor pods.

## How it works

- The gateway runs as a StatefulSet. The pod ordinal is the **slot**: pod `n`
  is tailnet node `speakeasy-gw-<n>` for every customer. The StatefulSet
  controller normally runs one pod per ordinal, so there is no lease and no
  fencing. A force-deleted or partitioned pod can briefly overlap with its
  replacement; Tailscale allows one live session per node key, which bounds
  that overlap.
- The reconciler (`gram network-ingress-worker`) creates one
  `network_ingress_nodes` row per slot, defines the Tailscale Service
  `svc:<label>` through the Tailscale API and mints a one-shot, preauthorized,
  tagged auth key for every slot that has no persisted state. The customer's
  OAuth client never leaves the control plane.
- Every 15s each pod lists enabled ingresses for its shard. For each it starts
  a `tsnet.Server` whose `ipn.StateStore` is backed by
  `network_ingress_node_states`. Restarts reconnect as the same device, so
  customers never see registration churn.
- The node advertises the service with `ListenService`. Requests arrive over
  tsnet's loopback proxy; the gateway resolves the peer with `WhoIs`, replaces
  every `Tailscale-*` header with the resolved identity and forwards to the
  private listener with the pod's projected service-account token, exactly as
  the attestor did.
- In Kubernetes, tsnet keeps each node's TLS certificate in the state store.
  Every slot of a connection serves the same `svc:<label>` name, so the
  certificate, its key and the ACME account that issues it are stored once
  per connection, in `network_ingress_certificate_states`, and shared by all
  slots. Let's Encrypt
  allows 50 new certificates per tailnet per week, shared with the customer's
  own use, and one certificate per connection keeps the gateway's share small.
- Each pod heartbeats its slot rows; the reconciler reports the ingress online
  when any slot has a fresh `online` heartbeat.
- A slot is `online` only when its node is connected **and** the tailnet has
  approved it to host `svc:<label>`, which control signals through the
  node's `service-host` capability. Advertising alone is not enough: a node
  whose service is unapproved, missing or misnamed connects without error but
  gets no traffic. Such a slot reports `awaiting_approval`, and the ingress
  shows `provider_service_not_approved` until the customer approves the host
  or fixes `autoApprovers`. Approval is re-checked on every heartbeat, so
  approving or revoking a host takes effect without a restart.
- An approved slot is `online` only once it also holds the TLS certificate
  for `svc:<label>`: without it every client handshake fails. Tailscale gets
  the certificate from Let's Encrypt, which allows a tailnet 50 new
  certificates a week, shared with everything else on that tailnet. tsnet
  only asks for one when a client connects or in an hourly refresh, so the
  gateway requests it as soon as the node is approved, waits briefly for it
  at start, and retries at Let's Encrypt's stated retry time when refused.
  Until it is issued the slot reports `awaiting_certificate`: its message says
  the request is in progress or, after a refusal, why and when it retries.
  The ingress shows `provider_certificate_unavailable`. Slots of one
  connection take turns requesting it, under a Postgres advisory lock, so
  the one that waited finds the certificate already stored.

## Encryption

Node state and auth keys are the gateway's most sensitive data: anyone holding
a node's state can act as that device on the customer's tailnet. Both are
sealed with organization encryption (`orgencryption`, use case
`tailscale-gateway-state`), never with the application key, and each value is
bound to its organization and column.

- The reconciler mints the organization's data key on first use, before it
  mints any auth key. The gateway only encrypts and decrypts.
- Both processes need the `GRAM_ORG_ENCRYPTION_*` settings. Without them the
  reconciler refuses to provision gateway ingresses and the gateway refuses to
  start; nothing falls back to the application key.
- When the organization's key is suspended or unreachable, provisioning
  reports `encryption_key_unavailable` without minting auth keys, and a host
  whose state cannot be read records `error` instead of starting. For a
  customer-managed key, the customer restores Speakeasy's access to it.
- The customer's Tailscale OAuth client (`network_ingresses.credentials_encrypted`)
  still uses the application key; it predates this package.

## Which ingresses use it

The `network_ingresses.runtime` column decides:

- `gateway`: hosted only by this gateway.
- `NULL` (or anything else): hosted only by the Tailscale Kubernetes operator,
  as before. Nothing in this package moves an ingress between the two.

Every new ingress is created with `gateway`. Ingresses created before the
gateway existed stay on the operator.
`GRAM_NETWORK_INGRESS_GATEWAY_ENABLED` tells the reconciler the gateway pods
are deployed; until it is set, gateway ingresses wait as
`provider_configuration_unavailable` instead of falling back to the operator.

## Running locally

The daemon needs a real tailnet, so it is opt-in. It also needs organization
encryption, which `./zero` configures (`mise run zero:org-encryption`).
Provision an ingress through
the dashboard with the `tag:speakeasy` policy, then set in `mise.local.toml`:

```toml
GRAM_TAILNET_GATEWAY_UPSTREAM_URL = "https://localhost:<private-listener-port>"
GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE = "<absolute path to the listener CA>"
GRAM_TAILNET_GATEWAY_TOKEN_PATH = "<absolute path to a file holding the attestation token>"
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
