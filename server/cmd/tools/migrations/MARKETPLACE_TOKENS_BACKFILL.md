# Marketplace token backfill

This offline application-data backfill moves plugin marketplace tokens out of the plaintext `plugin_github_connections.marketplace_token` column.

A marketplace token is the URL-as-secret credential in a marketplace install URL (`/marketplace/<token>.git`). Anyone who has it can clone the project's private marketplace repository. New publishes already store a token as:

- `marketplace_token_hash`: the hex SHA-256 the marketplace proxy looks the token up by.
- `marketplace_token_encrypted`: the token sealed under the platform key, so the server can show the install URL again.

This backfill gives every older connection the same form. For each connection that still holds a plaintext token, it:

1. Copies the project's organization onto the connection's `organization_id`. The organization encryption fence needs the organization on the row.
2. Seals the token, opens the sealed copy to check it, and writes the hash and copy.
3. Clears the plaintext column in the same statement.

The token itself does not change, so every install URL that admins and devices already hold keeps working. The run changes only the token columns and `organization_id`. It never touches `updated_at`, which is the last-published timestamp and part of the device agent's ETag. It does not alter the schema or publish events. It never prints a token.

## In dev and prod: use the admin dashboard

Apply it from the admin dashboard's temporary **Secret backfills** page, under Platform Management. The page previews the counts below and runs the same backfill inside the admin server, with the platform key and organization encryption the servers use. Nobody needs database write access or the key on their machine.

The page shows the admin server's platform key fingerprint. Check it matches the servers' `encryption key loaded` log before you apply. The checks under "Before running" still apply. When the admin server has organization encryption, it also seals the tokens of organizations past `platform_key` instead of leaving them in place.

The commands below run the same backfill from a terminal, for local development.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database.

## Before running

1. The application change that hashes new tokens and reads both forms is deployed everywhere: server and worker.
2. No image built before that change is still a rollback target. An older image reads only the plaintext column, so it cannot serve a moved token. This backfill is the point of no return for that rollback.
3. No organization has enrolled `marketplace-tokens` in organization encryption yet. This run seals with the platform key only. A token whose organization is past `platform_key` is counted under `needs_organization_key` and left in place.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations marketplace-tokens \
  -environment=staging
```

Counts connections. Writes nothing and needs no encryption key. A preview still sends credentials, so it requires TLS too unless every host in `GRAM_DATABASE_URL` is on this machine (loopback, `localhost`, or a Unix socket).

## Apply

```sh
GRAM_DATABASE_URL=... GRAM_ENCRYPTION_KEY=... go run ./cmd/tools/migrations marketplace-tokens \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-key-fingerprint=<fingerprint> \
  -confirm-apply=marketplace-tokens
```

- `-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. The URL must name a single host and require TLS (`sslmode=require`, `verify-ca` or `verify-full`).
- `GRAM_ENCRYPTION_KEY` must be the platform key the servers run with. `-confirm-key-fingerprint` must match its fingerprint, which every server logs at startup as `encryption key loaded`. The run clears the plaintext token after it writes the sealed copy, so a copy sealed under any other key would leave the server unable to show the URL again.
- `-batch-size` (default 500, at most 10000) bounds each attribution statement and each page of tokens. Each token moves in its own statement, so an interrupted run continues where it stopped when run again.

After applying, run the preview again and confirm `legacy=0`. If it is not 0, see the output section.

## Output

`mode=… environment=… attributed=N moved=N skipped=N needs_organization_key=N unattributed=N legacy=N hashed=N`

- `attributed`: connections this run gave an `organization_id`. Always 0 in a preview.
- `moved`: plaintext tokens this run moved. Always 0 in a preview.
- `skipped`: plaintext tokens that changed between the read and the write, such as when another run moved them first. Run again.
- `needs_organization_key`: plaintext tokens left in place because their organization's `marketplace-tokens` use case is past `platform_key`. They keep working through the plaintext fallback. Move them before that organization finalizes; finalize refuses while they remain.
- `unattributed`: connections still without an `organization_id`, counted after the run. After an apply, these are connections whose project's organization has no `organization_metadata` row (`projects.organization_id` has no foreign key). Their token, if any, stays in plaintext. Investigate any nonzero count.
- `legacy`: connections that still hold a plaintext token, counted after the run.
- `hashed`: connections whose token is stored as a hash and a sealed copy.

Counts never name organizations or connections. Do not record organization IDs in tickets or pull requests.

On failure the command prints how many connections it attributed and moved before the error and exits non-zero. Every statement commits on its own, so running again continues.

## Recovery

A moved token cannot be moved back by this tool, and none of its writes needs to be undone: the token, its install URL and every device keep working. The logs show any request that still resolves through the plaintext column (`marketplace token resolved from the plaintext column`), which stops once `legacy=0`.

If the server cannot open a moved token (for example, `GRAM_ENCRYPTION_KEY` was wrong despite the fingerprint check), the marketplace proxy still serves every clone, because it needs only the hash. Showing the install URL fails until the token is rotated.
