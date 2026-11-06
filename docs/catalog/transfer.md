# Internal published catalog synchronization

This is staff-only administration. The application continues consuming the same
catalog tables and assets. There is no public sync/feed, customer transfer API,
or tenant Platform MCP tool.

## Architecture and identity boundaries

A running **destination Admin service** owns its environment, database, blob
storage, public asset origin, and fixed production source configuration. Its
internal `POST /admin/catalog.sync` operation accepts only an empty JSON object.
It performs initial population or missing-entry catch-up through the reusable
registry importer. Requests cannot override any configuration or supply a bundle.
Only deployment environments `local` and `dev` can import. `prod`, empty, and
unknown environments reject before reading the body or fetching production data.

Staff browser requests use existing Admin OIDC sessions, live identity verification,
and the Admin origin/CSRF guard. Local CLI authentication uses the configured local
mock OIDC flow, with bounded allowed redirects and an in-memory cookie jar. It
creates its own ordinary local Admin session; it does not copy browser cookies.
Neither local CLI nor setup accepts writable database/blob credentials.

Production's internal `GET /admin/catalog.export` is read-only. It returns a
complete, versioned bundle with published records and curated SVG logo bytes.
Its narrowly scoped identity verifier accepts only Google-issued ID tokens with
the configured exact audience and explicitly allowlisted immutable subjects.
Export credentials do **not** grant Admin session/write authority. Customer API
keys, local mock identities, arbitrary Google accounts, and browser cookies alone
cannot authorize export. The route remains internal; network exposure is not an
authorization substitute.

The destination retrieves automatically from a startup-configured HTTPS endpoint.
It never fetches a request-supplied URL or logo URL. Redirects are refused, so an
export token cannot be redirected to another origin. A deployed `dev` service uses
its attached workload identity. Local Admin uses machine Application Default
Credentials (ADC) to obtain a short-lived ID token by impersonating the dedicated
catalog-reader service account. Token acquisition is lazy; normal local Admin
startup does not require production credentials or a successful cloud request.

## Configuration and local command

Production Admin startup accepts `GRAM_CATALOG_EXPORT_AUDIENCE` and
`GRAM_CATALOG_EXPORT_SUBJECTS` (comma-separated numeric service-account subjects).
Without export configuration no source route is registered. Partial configuration
fails closed. Export is enabled only in `prod`.

Destination Admin startup uses `GRAM_CATALOG_SOURCE_URL` (HTTPS URL ending exactly
in `/admin/catalog.export`), `GRAM_CATALOG_SOURCE_AUDIENCE`, and optional
`GRAM_CATALOG_SOURCE_READER`. The dedicated impersonated reader is required in
`local`; `dev` may use its attached workload identity. These are Admin startup
settings, not CLI flags or request fields. When absent, the sync operation returns
unavailable while normal Admin startup remains independent of cloud access.

With the configured local Admin and mock OIDC services running:

```sh
gram catalog-transfer
```

The local client reads `GRAM_ADMIN_SERVER_URL` and
`GRAM_ADMIN_OIDC_EMULATOR_URL`; both must be loopback origins. Local HTTPS must
use a certificate trusted by the machine; certificate checks are never disabled.
`GRAM_ADMIN_OIDC_MOCK_USER_EMAIL` may select an existing local mock identity.
The client defaults to the configured sample engineering identity, retains cookies
only in memory, and does not visit the post-login dashboard redirect. The command
has no database, asset, environment, bundle-file, or session-token options.
Non-loopback/Tailscale origins are intentionally refused by this local automation;
no remote mock-login fallback is attempted.

## Provisioning prerequisites — not performed by this change

These are rollout requirements, not claims of existing access:

1. Provision a dedicated catalog-export-reader service account with **no production
   database, asset-bucket, or general Admin permissions**.
2. Grant the approved developer group only ID-token minting permission for that
   reader identity (`iam.serviceAccounts.getOpenIdToken`, using an appropriate
   narrowly scoped role). No downloadable service-account key files.
3. Configure the production export audience and immutable subject allowlist for
   the reader identity and approved `dev` workload identity.
4. Configure destination source URL/audience and the local reader impersonation
   target. Ensure the relevant IAM API, ADC quota project, and API-use permission
   are available to the approved developer identity.
5. Confirm private network reachability from approved developer machines and the
   `dev` workload. Do not make the feed public to work around connectivity.
6. Establish machine ADC through the approved Google login onboarding once.
   Subsequent fresh worktrees use renewable machine credentials automatically;
   no per-worktree bundle transport or production database login is needed.

Request required access through **C1 (ConductorOne)**. No infrastructure changes,
cloud operations, IAM bindings, credentials provisioning, or production reads are
performed by this implementation. Google IAM/network behavior requires separate
rollout verification; local HTTP contract tests do not establish live access.

## Transfer guarantees

- Source uses a read-only repeatable-read snapshot and complete published records,
  not discovery's filtered projection. Invalid records or missing assets fail the
  export rather than silently producing an incomplete catalog.
- Bundles are bounded at 10,000 records and 64 MiB serialized JSON. Platform image
  limits and static SVG validation apply. Each shared logo is validated once per
  bundle, not once per reference; validation checks cancellation between records
  and assets.
- Only catalog definitions and referenced public platform SVGs are read. No
  tenant configuration, installations, credentials, or authorization tables are
  copied. Published definition metadata must not itself contain secrets; unknown
  publisher metadata is preserved rather than scanned or redacted.
- Default/dark curated icons become destination-owned platform assets with rewritten
  URLs. Upstream publisher `server.icons` metadata is retained but never fetched.
- Publication metadata is removed and recreated by normal destination creation.
  Unknown metadata and large numeric values remain intact.
- Every existing name, including unpublished entries and testing edits, is preserved
  and reported as a conflict. New entries are committed together; later business
  failures roll back publication. Name/endpoint immutability is never bypassed.
- Catalog writes are serialized during import; readers remain available. Blob
  uploads are not PostgreSQL-transactional and may leave unreferenced validated
  images after failure; content-addressed uploads are reused on retry.
- Global staff mutations use the existing structured Admin audit convention with
  verified actor and result counts, not invented tenant audit scope or raw bundles.

## Ticket boundaries and preview limitation

GRW-416 provides authenticated automatic retrieval and safe initial population /
missing-entry catch-up through reusable services and a local CLI. GRW-417 owns
refresh/baselines/conflict semantics; GRW-418 owns Admin preview/confirmation and
automatic fresh-worktree setup orchestration using this backend; GRW-419 owns reset.
No manual bundle or browser-cookie transportation is an accepted workflow.

Admin MCP import is not exposed: it must integrate the existing exact-change
proposal, staff approval, and durable execution-receipt lifecycle before becoming
a write tool. Source workload authentication never bypasses that lifecycle.

Preview databases are PR-specific clones initialized once and then reused;
nonproduction asset storage is shared. Updating staging (`dev`) does **not** update
existing preview databases. Shared-preview behavior remains unresolved. This change
assumes neither infrastructure changes nor per-preview synchronization.
