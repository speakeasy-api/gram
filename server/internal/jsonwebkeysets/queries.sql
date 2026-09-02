-- Loads everything the mint path needs in one read, before the write
-- transaction opens: the backing external key, its GCP resource name, and the
-- GCP identity of the credential that reaches it. The KMS read happens outside
-- any transaction (a low-quota management-tier RPC must not hold a pool
-- connection), so nothing here is locked; the write transaction re-locks the
-- key row and re-verifies it afterwards.
--
-- The subtype and credential joins are LEFT so the caller can tell the states
-- apart: a missing gcp_kms_keys row means the key is AWS-backed (refused with a
-- clear message), and a NULL credential_id means the backing credential was
-- soft-deleted (external_credentials.deleted is a generated column, so a soft
-- delete never fires the external_keys foreign key). The credential join
-- predicate spells out every condition a usable credential must meet so a row
-- failing one reads as an absent credential.
-- name: GetExternalKeyForMint :one
SELECT
  sqlc.embed(ek),
  gcp.resource_name,
  ec.id AS credential_id,
  gic.impersonate_service_account,
  gic.wif_pool_id,
  gic.wif_provider_id,
  gic.wif_project_number,
  gic.skip_project_verification
FROM external_keys AS ek
LEFT JOIN gcp_kms_keys AS gcp ON gcp.external_key_id = ek.id
LEFT JOIN external_credentials AS ec
       ON ec.id = ek.external_credential_id
      AND ec.organization_id = ek.organization_id
      AND ec.project_id IS NULL
      AND ec.provider = 'gcp_iam'
      AND ec.deleted IS FALSE
LEFT JOIN gcp_iam_credentials AS gic
       ON gic.external_credential_id = ec.id
WHERE ek.id = @id
  AND ek.organization_id = @organization_id
  AND ek.project_id IS NULL
  AND ek.deleted IS FALSE;

-- Locks the backing external key row for the duration of a JWKS write, as the
-- counterpart to the FOR UPDATE the externalkeys delete path takes
-- (LockExternalKeyForDelete): FOR SHARE here conflicts with that FOR UPDATE, so
-- a JWKS row can never be committed against a key whose delete is in flight,
-- and a delete can never commit between this check and the JWKS write.
-- external_keys.deleted is a generated column, so no foreign key would catch
-- that race.
--
-- project_id IS NULL is asserted here rather than assumed: both external_keys
-- and json_web_key_sets cascade-delete from projects, so letting an org-level
-- set reference a project-scoped key would entangle project deletion with
-- org-level state.
-- name: LockExternalKeyForJwksWrite :one
SELECT id, provider, algorithm
FROM external_keys
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
FOR SHARE;

-- name: CreateJsonWebKeySet :one
INSERT INTO json_web_key_sets (organization_id, external_key_id, name)
VALUES (@organization_id, @external_key_id, @name)
RETURNING *;

-- name: GetJsonWebKeySet :one
SELECT *
FROM json_web_key_sets
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE;

-- name: ListJsonWebKeySets :many
SELECT *
FROM json_web_key_sets
WHERE organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
ORDER BY id DESC;

-- name: UpdateJsonWebKeySet :one
UPDATE json_web_key_sets
SET name = @name,
    external_key_id = @external_key_id,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
RETURNING *;

-- Locks the set row to serialize every key write against every other and
-- against set deletion. Publishing, activating, retiring, revoking, and
-- deleting the set all take this lock first, which is what makes the ordered
-- lifecycle statements safe: without it, a concurrent publish could insert a
-- live key into a set mid-cascade-soft-delete (the plain UPDATE a soft delete
-- performs does not conflict with the FOR KEY SHARE an insert's foreign key
-- takes), stranding a live key under a deleted set and blocking the backing
-- external key's deletion forever.
-- name: LockJsonWebKeySetForKeyWrite :one
SELECT *
FROM json_web_key_sets
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
FOR UPDATE;

-- name: SoftDeleteJsonWebKeySet :one
UPDATE json_web_key_sets
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
RETURNING *;

-- Soft-deletes every live key in a set alongside the set itself. The foreign
-- key's ON DELETE CASCADE only fires on hard deletes, so without this a
-- soft-deleted set would leave live keys occupying json_web_keys_one_active_idx
-- and holding the backing external key's delete guard closed. RETURNING so the
-- caller emits one audit event per withdrawn key.
-- name: CascadeSoftDeleteJsonWebKeys :many
UPDATE json_web_keys
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE json_web_key_set_id = @json_web_key_set_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Lists a set's keys. Revoked keys are always also soft-deleted (revocation is
-- how a kid leaves the published set), so the default listing filters deleted
-- and include_revoked re-admits exactly the revoked rows to surface revocation
-- history. The parent-set join is belt and braces: the caller 404s on a
-- missing set first and the cascade soft-deletes keys with their set, but
-- key listings filter the parent's deleted flag themselves rather than trust
-- every caller to.
-- name: ListJsonWebKeys :many
SELECT k.*
FROM json_web_keys AS k
JOIN json_web_key_sets AS s
  ON s.organization_id = k.organization_id
 AND s.id = k.json_web_key_set_id
WHERE k.json_web_key_set_id = @json_web_key_set_id
  AND k.organization_id = @organization_id
  AND s.deleted IS FALSE
  AND (k.deleted IS FALSE OR (@include_revoked::boolean AND k.state = 'revoked'))
ORDER BY k.id DESC;

-- name: GetJsonWebKey :one
SELECT *
FROM json_web_keys
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: GetActiveJsonWebKey :one
SELECT *
FROM json_web_keys
WHERE json_web_key_set_id = @json_web_key_set_id
  AND organization_id = @organization_id
  AND state = 'active'
  AND deleted IS FALSE;

-- Reports whether a kid has ever been published into the set, deliberately
-- including soft-deleted (revoked) rows. json_web_keys_set_kid_idx only
-- enforces uniqueness among live rows, so after a revoke the kid is free at the
-- index level — but a kid is a pure function of the key material, and verifiers
-- may have negatively cached a revoked kid, so republishing the same backing
-- key would silently un-revoke it from their perspective. Revocation is
-- permanent per (set, kid).
-- name: JsonWebKeyKidExistsInSet :one
SELECT EXISTS (
  SELECT 1
  FROM json_web_keys
  WHERE json_web_key_set_id = @json_web_key_set_id
    AND organization_id = @organization_id
    AND kid = @kid
) AS kid_exists;

-- name: CreateJsonWebKey :one
INSERT INTO json_web_keys (
  organization_id,
  json_web_key_set_id,
  external_key_id,
  state,
  kid,
  public_jwk,
  activated_at
) VALUES (
  @organization_id,
  @json_web_key_set_id,
  @external_key_id,
  @state,
  @kid,
  @public_jwk,
  CASE WHEN @state::text = 'active' THEN clock_timestamp() END
)
RETURNING *;

-- Retires the set's currently active key, excluding the key being activated so
-- an activate of the already-active key cannot retire it. Run as the first of
-- two ordered statements under LockJsonWebKeySetForKeyWrite:
-- json_web_keys_one_active_idx is non-deferrable, so the current active key
-- must leave 'active' before the next one enters it. At most one row matches by
-- construction of that index.
-- name: RetireActiveJsonWebKey :one
UPDATE json_web_keys
SET state = 'retired',
    retired_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE json_web_key_set_id = @json_web_key_set_id
  AND organization_id = @organization_id
  AND state = 'active'
  AND deleted IS FALSE
  AND id <> @exclude_id
RETURNING *;

-- name: ActivateJsonWebKey :one
UPDATE json_web_keys
SET state = 'active',
    activated_at = clock_timestamp(),
    retired_at = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND state IN ('pending', 'retired')
  AND deleted IS FALSE
RETURNING *;

-- name: RetireJsonWebKey :one
UPDATE json_web_keys
SET state = 'retired',
    retired_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND state = 'active'
  AND deleted IS FALSE
RETURNING *;

-- Revocation withdraws the key from the published set entirely, so the row is
-- soft-deleted in the same statement: state = 'revoked' rows are always also
-- deleted rows, which is what drops them from default listings and from the
-- externalkeys delete guard's live-reference test.
-- name: RevokeJsonWebKey :one
UPDATE json_web_keys
SET state = 'revoked',
    revoked_at = clock_timestamp(),
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Counts the live remote_session_clients still pointing at a set. Backs both the
-- delete preflight and the refusal inside DeleteSet, which run the same query so
-- the preflight cannot disagree with the mutation it predicts.
--
-- Run inside the delete transaction, after LockJsonWebKeySetForKeyWrite. The
-- attach side takes FOR SHARE on the same row (LockJsonWebKeySetForClientAttach
-- in remotesessions), so an attach either lands before this count sees it or
-- blocks until the delete commits and then fails its own live-set lookup.
--
-- The database will not enforce this. remote_session_clients_json_web_key_set_tenant_fkey
-- omits ON DELETE, which is NO ACTION, and `deleted` is a generated column, so
-- nothing fires on the soft-delete path. Soft-deleted clients do not count: a
-- tombstoned client never authenticates again and must not pin a set forever.
--
-- A count rather than EXISTS because the preflight needs the number anyway, and
-- COUNT(*) types as a non-nullable int64 — no fail-open reading of an unset
-- pgtype.Bool, which is the trap the externalkeys guard documents.
-- name: CountRemoteSessionClientsForJsonWebKeySet :one
SELECT COUNT(*)
FROM remote_session_clients
WHERE organization_id = @organization_id::text
  AND json_web_key_set_id = @json_web_key_set_id::uuid
  AND deleted IS FALSE;

-- The impact summary the delete preflight reports: how many live clients
-- reference the set, and a capped list of them to name.
--
-- One statement rather than a count plus a listing. Two statements cannot be
-- made consistent by wrapping them in a transaction, because PostgreSQL's
-- default READ COMMITTED gives every statement its own snapshot, so a client
-- attaching or detaching between them would let the preflight report a count
-- its own list contradicts. Raising the isolation level would also work, but
-- then the guarantee lives in the caller and silently disappears if anyone
-- changes how the transaction is opened; here it is structural.
--
-- The count is unbounded and authoritative while the list is capped, so a set
-- referenced by more clients than the cap reports a truncated list against a
-- full count. array_agg returns NULL over an empty group, hence the COALESCE.
-- Ordered oldest first so the listing is stable across calls.
--
-- The slice is applied to the finished aggregate, so array_agg does build all N
-- elements before 50 survive. That is deliberate. Capping inside instead (a
-- LIMIT 50 subquery beside a separate COUNT) was measured and is worse: the
-- index is (organization_id, json_web_key_set_id) and does not cover
-- created_at/id, so the ordered listing sorts every matching row either way,
-- and the capped form pays a second scan of the same rows to do it. At 5000
-- references it ran 2.75ms against 0.96ms with double the buffer reads; at a
-- realistic 5 it was a wash on time and still double the buffers. The only
-- thing it improves is sort memory, 458kB down to 28kB, and N cannot get large
-- here: json_web_key_set_id is only ever set by attachKeySet, one client per
-- call, behind the customer_managed_encryption_keys entitlement, so references
-- accrue one deliberate administrator action at a time.
-- name: SummarizeRemoteSessionClientsForJsonWebKeySet :one
SELECT
    COUNT(*) AS client_count,
    COALESCE(
        (array_agg(client_id ORDER BY created_at, id))[1:sqlc.arg('limit_value')::int],
        '{}'::text[]
    )::text[] AS client_ids
FROM remote_session_clients
WHERE organization_id = @organization_id::text
  AND json_web_key_set_id = @json_web_key_set_id::uuid
  AND deleted IS FALSE;
