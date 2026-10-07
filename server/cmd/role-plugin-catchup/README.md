# One-off role-plugin catch-up (DNO-1247)

Run **after flag-free setup is deployed to the event consumers** and before the
separate cleanup of stored flag rows. Earlier execution would let the old
consumers acknowledge and skip setup for organizations without the flag.

This script queues the existing organization setup for active organizations with
no active `automatic-role-distribution` flag, including soft-deleted flags. It
does not create plugins itself, change flags, track completion, or retry work.
The flag identifies organizations outside the enabled staff rollout; it is not
evidence that previously enabled organizations completed setup. New organizations
created after flag removal may also be selected; existing setup handles replay.

## Operator execution

Requires separate production authorization. A human runs this **locally**, using
the existing connection task in the adjacent `gram-infra` checkout. Nothing is
installed in the production server or registered as a new CLI command.

From `gram-infra`, substitute the absolute path to this Speakeasy checkout:

```sh
# Preview only; use existing read access and answer n to the grant prompt.
mise run gcp:db:master prod gram -- bash -c \
  'cd /absolute/path/to/gram && mise exec -- go run ./server/cmd/role-plugin-catchup'

# After reviewing the count, use existing authorized write access; answer n.
mise run gcp:db:master prod gram -- bash -c \
  'cd /absolute/path/to/gram && mise exec -- go run ./server/cmd/role-plugin-catchup -apply'
```

The explicit `gram` argument is required: without it, the task interprets
`bash` as the database name. This invocation asks **"Grant permissions first?
(y/n)"**, not READ/ALL. Answer **n** to use existing authorized grants. Answering
y grants ALL on every table and sequence, including DELETE; that is not needed
for this catch-up. If the necessary grants are absent, stop and obtain the
appropriate authorization rather than selecting y as a workaround.

The existing task opens a local Cloud SQL proxy, provides `DB_URL`/PostgreSQL
connection settings, and closes the proxy when the command exits. The operator
needs the existing Google Cloud access prerequisites and schema USAGE. Preview
requires SELECT on `organization_metadata`/`organization_features`; apply also
requires SELECT/INSERT on `publish_outbox`. Stored flag deletion is deliberately
separate; this script does not require DELETE.

All setup requests commit in one transaction. Preview writes nothing. Queueing
is not completion: use the existing event-processing logs for processing errors.
A repeated apply queues another pass; there is no completed-once marker. Do not
use it as a recurring task. No production access is needed for its integration
test:

```sh
mise run test:server ./cmd/role-plugin-catchup
```
