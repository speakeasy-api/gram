# Gateway certificates and Let's Encrypt limits

Each private network connection is served by several gateway slots (3 by
default), all advertising the same Tailscale Service, `svc:<label>`. Clients
connect over HTTPS, so every slot needs a certificate for
`<label>.<tailnet>.ts.net`. Tailscale obtains it from Let's Encrypt. This
document explains why the gateway coordinates that so carefully, and how.

## The constraint

Let's Encrypt's [rate limits](https://letsencrypt.org/docs/rate-limits/) that
matter here:

| Limit                           | Size        | Refills         | Notes                                                                                                             |
| ------------------------------- | ----------- | --------------- | ----------------------------------------------------------------------------------------------------------------- |
| New certificates per tailnet    | 50 / 7 days | 1 every 202 min | `ts.net` is a public suffix, so the tailnet is the domain. Shared with everything else on the customer's tailnet. |
| New certificates per exact name | 5 / 7 days  | 1 every 34 h    | Issuing the same hostname again without renewing counts.                                                          |
| ARI renewals                    | none        |                 | An order that names the certificate it replaces, which has not already been replaced, is exempt from every limit. |

A refused order means every handshake on that connection fails until the
limit refills: hours for the tailnet limit, days for the per-name one. The
gateway cannot raise these limits or see the customer's own use of them. So
the rule is: **one new certificate per connection, ever, and every renewal an
ARI renewal.**

## What goes wrong without coordination

Each was seen on a real tailnet, reproduced in a test or found by model checking.

1. **Every slot orders its own certificate.** tsnet stores certificates per
   node, so N slots means N certificates, and N again on every renewal.
   _Fix: one shared store per connection._
2. **Slots race on first issuance.** With nothing stored, every slot orders at
   once. _Fix: an issuance lease._
3. **tsnet orders on a client handshake.** When tsnet terminates TLS, a client
   reaching a slot with no stored certificate makes tsnet order one there and
   then, outside any lease. Reproduced in `issuancetest`. _Fix: the gateway
   terminates TLS._
4. **tsnet renews per slot, on a cached schedule.** Each slot's tsnet picks a
   renewal time once per certificate and renews when it passes, so every slot
   renews once per cycle, and none notices Let's Encrypt moving a window
   earlier (as it does before a mass revocation). _Fix: the gateway decides
   when to renew._

## How the gateway coordinates

**Shared store.** The certificate, its key and the ACME account key live once
per connection in `network_ingress_certificate_states`, sealed with the
organization's key, and every slot reads them (`statestore.go`). tsnet uses
the state store for certificates only when it detects Kubernetes; local
development keeps them in the state directory instead.

**Issuance lease.** Ordering a missing certificate or renewing one requires
the `issuance-lease` row in the same table, claimed with a conditional upsert
and released by deleting only the caller's own claim. It holds no database
connection. A slot that finds it taken retries in 5 s and then finds the
certificate stored. A valid stored certificate is reused without the lease, so
restarting a pod takes none. The lease (3 min) must outlast one request
(2 min) plus its release (10 s); `TestCertificateLeaseOutlastsALiveRequest`
holds that.

**TLS in the gateway.** The service is opened in TCP mode; tsnet forwards each
connection with a PROXY v2 header naming the peer, and the gateway terminates
TLS with only the certificate its tracker holds (`serviceMode`,
`tsnet_runtime.go`). A handshake before then fails; nothing is ordered.
`TestServiceModeLeavesTLSToTheGateway` guards the listen mode.

**Renewal by the gateway** (`renewal.go`). A slot holding a certificate:

- re-reads the store every hour (±10%) and adopts a newer certificate another
  slot stored, without calling tsnet;
- asks Let's Encrypt for the certificate's ARI renewal window when it is
  first held and then on `Retry-After`, clamped to 1-6 h;
- renews at one point in that window, derived from the serial number so every
  slot agrees, or at two thirds of the lifetime if the window cannot be read;
- renews under the lease after re-reading the store, by asking tsnet for a
  certificate valid longer than the current one (`CertPairWithValidity`),
  which makes tsnet renew at once and name the replaced certificate;
- on failure keeps serving the current certificate and backs off (30 s
  doubling to 30 min; a rate limit waits for the time Let's Encrypt names).

Certificate work is bounded to 8 concurrent requests per pod.

**State machine** (`certchart.go`). One `sc` statechart instance per node,
driven by the node's heartbeat: each poll fires the timers that are due, and
the chart starts at most one request at a time on its own goroutine.

| State             | Meaning                                                     |
| ----------------- | ----------------------------------------------------------- |
| `waiting`         | Nothing asked yet; the first poll after approval obtains    |
| `obtaining`       | No certificate held; obtaining one                          |
| `retrying`        | No certificate held; the last request failed                |
| `serving`         | Holding a certificate; waiting for the next deadline        |
| `checking_store`  | Re-reading the store for another slot's renewal             |
| `fetching_window` | Asking Let's Encrypt for the renewal window                 |
| `renewing`        | Renewing under the lease                                    |
| `renew_backoff`   | The last renewal failed; still serving the held certificate |
| `stopped`         | The node is closing                                         |

From `serving`, three deadlines lead out: the store refresh, the window
check and the renewal point, in that order when due together. A renewal is
adopted only if it is later than the held certificate. Expiry is not a
state; `certStatus` works it out from the clock. A slot without a valid
certificate reports `awaiting_certificate`, and the ingress shows
`provider_certificate_unavailable` with the reason and retry time.

## Not handled

- **Recreating a connection with the same hostname** deletes its stored
  certificate, so the next one is a new order against the 5-per-name limit.
- **The customer's own use** of the tailnet's 50 is outside our control; first
  issuance can be refused until the limit refills.
- **Local development** stores certificates per node, so its single slot is
  coordinated by tsnet's own lock, not the shared store.

## Observability

- `gram.tailnet_gateway.certificate.remaining`: seconds until each running
  node's certificate expires, by organization, ingress and slot. Warn when the
  minimum per ingress falls under a sixth of the lifetime (15 days for 90-day
  certificates), page under 3 days, and alert on no data.
- `gram.tailnet_gateway.certificate.requests`: obtain, renew and window
  requests by result (`ok`, `rate_limited`, `waiting_for_slot`, `error`).
  Sustained `renew` errors precede a low remaining time by days.

## Verification

- Unit tests: the chart, step by step (`certchart_internal_test.go`), lease and
  renewal coordination across slots (`certificate_internal_test.go`), the ARI
  client (`renewal_internal_test.go`), and the lease in Postgres
  (`statestore_test.go`).
- `issuancetest` (opt-in, `-tags tailnetgateway_issuance`): real tsnet nodes
  against Tailscale's test control server and a Pebble ACME server. It shows
  one certificate per connection under concurrent handshakes, and that tsnet
  renews at once and names the replaced certificate.
- The chart's own rules (`certchart_internal_test.go`): `Machine.Explore`
  walks every state the chart reaches under the test's request outcomes,
  with states merged by the test's key (times kept relative to now), and
  checks that a node holding a certificate never obtains one, renews only
  once the window says so, keeps its certificate through failed requests,
  and replaces one only with a later one.
- `Certificate.tla` models every slot of a connection together with the
  store, the lease, tsnet and Let's Encrypt, and checks one new order per
  connection, one renewal per certificate, and that every slot converges on
  the latest certificate. Each design ruled out above, and each contract on
  tsnet and the lease, is a constant that makes TLC print its trace.
  `Certificate.cfg` runs in seconds; `CertificateLarge.cfg` in about a minute.
  CI does not run them, so rerun both when the chart changes:

  ```bash
  java -XX:+UseParallelGC -cp tla2tools.jar tlc2.TLC -metadir "$(mktemp -d)" \
    -workers auto -config Certificate.cfg Certificate.tla
  java -XX:+UseParallelGC -cp tla2tools.jar tlc2.TLC -metadir "$(mktemp -d)" \
    -workers auto -config CertificateLarge.cfg Certificate.tla
  ```
