# Directory ID attribution

This staff-run command processes one exact Gram organization. It queries WorkOS for the complete directory, user, and group inventory before changing data. It only attributes existing rows whose directory ID is null; unmatched IDs, including tombstones, remain untouched. Local examples require a local database and a local WorkOS stub, never shared services or customer tenants.

Dry-run (default, executes changes in a transaction and rolls them back):

```sh
cd server
GRAM_ENVIRONMENT=local GRAM_DATABASE_URL=<LOCAL_DATABASE_URL> WORKOS_API_KEY=<LOCAL_STUB_KEY> WORKOS_API_URL=<LOCAL_WORKOS_URL> \
  mise exec -- go run ./cmd/directory-id-backfill --organization-id <ORG_ID>
```

Apply matched attributions:

```sh
cd server
GRAM_ENVIRONMENT=local GRAM_DATABASE_URL=<LOCAL_DATABASE_URL> WORKOS_API_KEY=<LOCAL_STUB_KEY> WORKOS_API_URL=<LOCAL_WORKOS_URL> \
  mise exec -- go run ./cmd/directory-id-backfill --organization-id <ORG_ID> --apply
```

External WorkOS endpoints require HTTPS and retain the default private-network blocklist. Only a loopback endpoint with `GRAM_ENVIRONMENT=local` permits the local stub, including HTTP. Use a stub key, not a real WorkOS API key, for local testing.

Review the JSON report before applying. `unmatched` lists only kind-prefixed WorkOS IDs. Investigate any residuals separately; this command does not create, restore, deactivate, link, or otherwise repair directory records.

The command snapshots the organization event cursor before fetching inventory, then takes the same transaction lock as the updated worker and checks the cursor again before writing. An event processed during retrieval, including a replay, rejects the run without attribution writes. Rerun with fresh inventory and review residuals; rejection does not mean the organization is covered. Updates are batched by directory and source type. An output failure after apply explicitly reports that writes have already committed.

## Rollout checklist

- Apply the additive directory ID schema migration first.
- Build this command from the application revision, then run dry-run and apply for each directory-synced organization before deploying the new worker behaviour. Retain reports in private operator storage, not Git or PR comments.
- Old workers do not take the new attribution lock. Pause organization event processing while applying attribution during a mixed-version rollout, then reconcile any deletions across the inventory/attribution window before resuming and considering an organization covered. The updated worker and command coordinate concurrent transactions; they do not repair historical deletions that missed unattributed rows. Merely replaying such a deletion may be skipped by its existing event guard.
- Treat unmatched residuals as unresolved attribution, not proof of deletion. Deleted directories are absent from live inventories, so their old rows may remain unattributed. Investigate with authoritative evidence through a separately reviewed remediation; never delete or attribute them by name or guess.
- Confirm new entity events persist directory IDs and directory deletion deactivates only the matching sources. Independent membership and direct role assignments must remain unchanged.

The command performs no WorkOS writes. Only an operator should execute a rollout against a shared environment; agents must use local services only.
