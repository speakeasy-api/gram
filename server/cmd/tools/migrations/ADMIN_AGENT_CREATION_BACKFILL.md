# Admin agent-creation initialization

This offline application-data operation initializes organization-wide `agent:create` for one explicitly selected organization's live global Admin role. It does not change Member or custom-role grants, does not change the schema, and never runs as continuous reconciliation.

## Deployment precondition

Build this tool from the release containing owner-scoped creation. Inventory existing organizations and apply the initialization to each intended organization **before enabling the creation change or exposing grant editing for that change**. The first initialization cannot distinguish an old missing default from a deliberate removal made before a completion receipt existed. Do not run it indiscriminately after administrators have started changing creation eligibility.

New organization provisioning records the same completion receipt. Once recorded, deleting the creation grant is permanent across tool reruns and ordinary reseeding.

## Preview and apply

Run from `server/`, with `GRAM_DATABASE_URL` supplied securely in the environment. Preview is the default; it rolls back any writes produced by initialization. An existing creation grant skips the grant write, and a completed initialization skips both grant and audit writes.

```sh
mise exec -- go run ./cmd/tools/migrations admin-agent-creation \
  --environment=<ENVIRONMENT> --organization=<ORG_ID>
```

Apply requires all four exact confirmations. The database target is the host, port, and database parsed from `GRAM_DATABASE_URL`, not a URL containing credentials.

```sh
mise exec -- go run ./cmd/tools/migrations admin-agent-creation \
  --environment=<ENVIRONMENT> --organization=<ORG_ID> --apply \
  --confirm-environment=<ENVIRONMENT> --confirm-organization=<ORG_ID> \
  --confirm-target=<HOST>:<PORT>/<DATABASE> \
  --confirm-apply=admin-agent-creation
```

The command commits one organization transaction. It adds the wildcard creation grant only if the live global Admin principal has no stored creation grant; existing narrowed creation grants are preserved. It records a completion audit even when the grant already exists. No customer webhooks are emitted. Output reports `grant_added`; a completed rerun reports `false` and changes nothing.

The existing organization configuration row lock serializes initialization, provisioning, and Admin grant patches. Grant and completion audit either commit together or both roll back. On an ambiguous commit error, rerun the same explicit target: a committed receipt prevents resurrection even if the grant has since been removed.

The completion receipt is an `access_role:update` audit by the `admin-agent-creation-initialization` system actor. **Retain this audit row permanently**: deleting it removes the tool's proof that initialization already happened. No general migration receipt table or new schema is introduced.
