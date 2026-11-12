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
- The node advertises the service with `ListenService` in TCP mode: tsnet
  forwards each connection over loopback with a PROXY protocol header naming
  the tailnet peer, and the gateway terminates TLS itself. It resolves the
  peer with `WhoIs`, replaces
  every `Tailscale-*` header with the resolved identity and forwards to the
  private listener with the pod's projected service-account token, exactly as
  the attestor did.
- Certificates are covered in detail in [CERTIFICATES.md](CERTIFICATES.md):
  the Let's Encrypt limits and how slots coordinate issuance and renewal.
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
  certificates a week, shared with everything else on that tailnet. The
  gateway requests it as soon as the node is approved, waits briefly for it
  at start, retries at Let's Encrypt's stated retry time when refused, and
  asks again hourly to pick up renewals. Handshakes are served only with the
  certificate the gateway obtained, and fail until it has one. If tsnet
  terminated TLS it would order a certificate for any client that connects
  before one is stored, outside the lease below, so each slot a client
  reached would spend one; `issuancetest` reproduces that against a real
  tsnet node and a Pebble ACME server.
  Until it is issued the slot reports `awaiting_certificate`: its message says
  the request is in progress or, after a refusal, why and when it retries.
  The ingress shows `provider_certificate_unavailable`. A slot reuses a
  stored certificate directly; to request a missing one it first claims a
  short lease row in `network_ingress_certificate_states`, so slots of one
  connection take turns and the one that waited finds the certificate
  already stored. The lease holds no database connection, so a pod
  starting hundreds of connections cannot exhaust its pool.
- The gateway, not tsnet, renews. Each slot reads Let's Encrypt's renewal
  window for its certificate (ARI) on `Retry-After`, at most every 6 hours,
  and renews at a point in the window derived from the serial number, so all
  slots agree. The due slot renews under the same lease after re-reading the
  store; the others pick the renewal up from the store within the hour. A
  failed renewal keeps the current certificate and backs off.
- Alerting: `gram.tailnet_gateway.certificate.remaining` is the seconds until
  each running node's certificate expires, by organization, ingress and slot.
  Alert on its minimum per ingress below a sixth of the lifetime (15 days for
  90-day certificates), page below 3 days, and alert on no data, since a
  connection no pod hosts reports nothing. `gram.tailnet_gateway.certificate.requests`
  counts obtain, renew and window requests by result (`ok`, `rate_limited`,
  `waiting_for_slot`, `error`); a run of renew errors precedes a low
  remaining time by days.

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

A new ingress is created with `gateway` only when its organization has the
`private-network-gateway` PostHog flag (targeted by organization slug), and
with `NULL` otherwise, including when the flag cannot be read. The choice is
fixed at creation, so changing the flag never moves an ingress; the API
returns it as `runtime`, and the dashboard's setup sheet reads the same flag
to show matching steps. Local development sets the flag on.
`GRAM_NETWORK_INGRESS_GATEWAY_ENABLED` tells the reconciler the gateway pods
are deployed; until it is set, gateway ingresses wait as
`provider_configuration_unavailable` instead of falling back to the operator,
so target the flag only once the gateway runs in that environment.

## Running locally

The whole path (tailnet client → gateway → private listener → MCP handler)
runs without Kubernetes in the opt-in `private-network` pitchfork group:

- `network-ingress-server`: the private listener on
  `https://localhost:$GRAM_NETINGRESS_PORT`, with its own mkcert certificate.
- `network-ingress-worker`: the reconciler, gateway provider only, one slot.
- `tailnet-gateway`: this daemon, forwarding to the listener.

In a cluster the gateway attests with its projected service-account token and
the listener checks it with Kubernetes TokenReview. Locally the gateway
presents an HMAC-signed token with the same claims (subject
`system:serviceaccount:gram-local:gram-tailnet-gateway`, audience
`gram-netingress`, 24-hour expiry), and the listener verifies it with
`netingress.LocalTokenReviewer`. Everything after the token review is the
production code path. The key and token live in `local/netingress/`, are
created on first start, and the token is re-minted on every gateway start.
The listener accepts local tokens only in the `local` environment.

1. Make sure `./zero` has configured organization encryption
   (`mise run zero:org-encryption`).
2. Grant your local organization the `network_ingress` feature on its
   Features page in the admin dashboard.
3. Start the group: `pitchfork start --group private-network`.
4. On the dashboard's Domains page, connect Tailscale with an OAuth client
   from a test tailnet and the `tag:speakeasy` policy the setup sheet shows.

Readiness is served on `http://127.0.0.1:$GRAM_NETINGRESS_CONTROL_PORT/healthz`
(which also reviews the gateway's token),
`http://127.0.0.1:$GRAM_NETWORK_INGRESS_WORKER_CONTROL_PORT/healthz` and
`http://localhost:$GRAM_TAILNET_GATEWAY_CONTROL_PORT/readyz`. To point the
gateway at another listener, set `GRAM_TAILNET_GATEWAY_UPSTREAM_URL` and
`GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE` in `mise.local.toml`.

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
