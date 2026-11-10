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
-- failing one reads as an absent credential. A platform-tier credential
-- (organization_id IS NULL) joins only for managed keys.
-- name: GetExternalKeyForMint :one
SELECT
  sqlc.embed(ek),
  gcp.resource_name,
  is_internal_session_issuer_external_key(ek.organization_id, ek.id) AS ema_issuer_managed,
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
      AND (
        ec.organization_id = ek.organization_id
        OR (ec.organization_id IS NULL AND (ek.identity_provider_connection_id IS NOT NULL OR is_internal_session_issuer_external_key(ek.organization_id, ek.id)))
      )
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
SELECT ek.id, ek.provider, ek.algorithm, ek.identity_provider_connection_id,
  is_internal_session_issuer_external_key(ek.organization_id, ek.id) AS ema_issuer_managed
FROM external_keys AS ek
WHERE ek.id = @id
  AND ek.organization_id = @organization_id
  AND ek.project_id IS NULL
  AND ek.deleted IS FALSE
FOR SHARE OF ek;

-- name: CreateJsonWebKeySet :one
INSERT INTO json_web_key_sets (organization_id, external_key_id, name, identity_provider_connection_id)
VALUES (@organization_id, @external_key_id, @name, sqlc.narg('identity_provider_connection_id'))
RETURNING *;

-- name: GetJsonWebKeySet :one
SELECT *
FROM json_web_key_sets
WHERE json_web_key_sets.id = @id
  AND json_web_key_sets.organization_id = @organization_id
  AND json_web_key_sets.project_id IS NULL
  AND NOT is_internal_session_issuer_key_set(json_web_key_sets.organization_id, json_web_key_sets.id)
  AND json_web_key_sets.deleted IS FALSE;

-- Sets left behind by a tombstoned identity provider connection are hidden;
-- they stay reachable by id so the organization can delete them.
-- name: ListJsonWebKeySets :many
SELECT *
FROM json_web_key_sets AS s
WHERE s.organization_id = @organization_id
  AND s.project_id IS NULL
  AND NOT is_internal_session_issuer_key_set(s.organization_id, s.id)
  AND s.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM identity_provider_connections AS ipc
    WHERE ipc.id = s.identity_provider_connection_id
      AND ipc.organization_id = s.organization_id
      AND ipc.deleted IS TRUE
  )
ORDER BY s.id DESC;

-- name: UpdateJsonWebKeySet :one
UPDATE json_web_key_sets
SET name = @name,
    external_key_id = @external_key_id,
    updated_at = clock_timestamp()
WHERE json_web_key_sets.id = @id
  AND json_web_key_sets.organization_id = @organization_id
  AND json_web_key_sets.project_id IS NULL
  AND NOT is_internal_session_issuer_key_set(json_web_key_sets.organization_id, json_web_key_sets.id)
  AND json_web_key_sets.deleted IS FALSE
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
WHERE json_web_key_sets.id = @id
  AND json_web_key_sets.organization_id = @organization_id
  AND json_web_key_sets.project_id IS NULL
  AND NOT is_internal_session_issuer_key_set(json_web_key_sets.organization_id, json_web_key_sets.id)
  AND json_web_key_sets.deleted IS FALSE
FOR UPDATE OF json_web_key_sets;

-- name: SoftDeleteJsonWebKeySet :one
UPDATE json_web_key_sets
SET deleted_at = clock_timestamp()
WHERE json_web_key_sets.id = @id
  AND json_web_key_sets.organization_id = @organization_id
  AND json_web_key_sets.project_id IS NULL
  AND NOT is_internal_session_issuer_key_set(json_web_key_sets.organization_id, json_web_key_sets.id)
  AND json_web_key_sets.deleted IS FALSE
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

-- Live clients still pointing at a set. Backs both the delete preflight and the
-- refusal inside DeleteSet, over the same predicate, so the preflight cannot
-- disagree with the mutation it predicts.
--
-- Runs after LockJsonWebKeySetForKeyWrite; the attach side takes FOR SHARE on
-- that row, so an attach either lands before this count or blocks and then
-- finds the set gone. The database cannot enforce this itself: the foreign key
-- is NO ACTION and `deleted` is generated, so soft deletes never fire it.
--
-- COUNT(*) rather than EXISTS: the preflight needs the number, and it types as
-- a non-nullable int64, which cannot fail open the way an unset pgtype.Bool can.
-- name: CountRemoteSessionClientsForJsonWebKeySet :one
SELECT COUNT(*)
FROM remote_session_clients
WHERE organization_id = @organization_id::text
  AND json_web_key_set_id = @json_web_key_set_id::uuid
  AND deleted IS FALSE;

-- The delete preflight's impact summary: how many live clients reference the
-- set, plus a capped list to name them. The count is authoritative; the list
-- truncates past the cap. Oldest first so it is stable across calls, and
-- COALESCE because array_agg is NULL over an empty group.
--
-- One statement, because READ COMMITTED snapshots per statement: a count and a
-- listing run separately could disagree, and a transaction around them does not
-- help. The cap is applied to the finished aggregate on purpose. Capping inside
-- reads better and measures worse, since the index cannot order this and the
-- sibling COUNT(*) scans everything regardless; see 94918ed for the numbers.
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

-- The organization's live issuer row, enabled or disabled. A disabled
-- organization has none.
-- name: GetOrganizationInternalSessionIssuer :one
SELECT isi.*
FROM internal_session_issuers AS isi
JOIN organization_metadata AS o ON o.id = isi.organization_id AND o.disabled_at IS NULL
WHERE isi.organization_id = @organization_id
  AND isi.deleted IS FALSE;

-- Serializes issuer creation per organization. NO KEY UPDATE leaves the
-- organization's foreign-key-checked inserts unblocked.
-- name: LockOrganizationForEMAIssuer :one
SELECT id
FROM organization_metadata
WHERE id = @organization_id
  AND disabled_at IS NULL
FOR NO KEY UPDATE;

-- The issuer URL is pinned here and never recomputed from configuration.
-- name: CreateInternalSessionIssuer :one
INSERT INTO internal_session_issuers (organization_id, json_web_key_set_id, issuer)
VALUES (@organization_id, @json_web_key_set_id, @issuer)
RETURNING *;

-- Enables or disables signing; discovery keeps serving either way.
-- name: SetInternalSessionIssuerDisabled :one
UPDATE internal_session_issuers
SET disabled_at = CASE WHEN @disabled::boolean THEN COALESCE(disabled_at, clock_timestamp()) ELSE NULL END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Serializes every key write on the issuer's key set, like
-- LockJsonWebKeySetForKeyWrite does for customer sets.
-- name: LockOrganizationEMAIssuerForKeyWrite :one
SELECT s.*
FROM json_web_key_sets AS s
JOIN internal_session_issuers AS isi
  ON isi.organization_id = s.organization_id
 AND isi.json_web_key_set_id = s.id
 AND isi.deleted IS FALSE
WHERE s.organization_id = @organization_id
  AND s.project_id IS NULL
  AND s.deleted IS FALSE
FOR UPDATE OF s;

-- name: SetOrganizationEMAIssuerBackingKey :one
UPDATE json_web_key_sets
SET external_key_id = @external_key_id,
    updated_at = clock_timestamp()
WHERE json_web_key_sets.id = @id
  AND json_web_key_sets.organization_id = @organization_id
  AND EXISTS (SELECT 1 FROM internal_session_issuers AS isi
    WHERE isi.organization_id = json_web_key_sets.organization_id
      AND isi.json_web_key_set_id = json_web_key_sets.id AND isi.deleted IS FALSE)
  AND json_web_key_sets.deleted IS FALSE
RETURNING *;

-- The organization's pinned issuer URL and its public verification keys,
-- served whether or not signing is disabled. Pending keys are published before
-- activation; a retired key stays only for the overlap window, then drops out
-- even before it is revoked. Ordered by immutable key id so the document and
-- its ETag are stable.
-- name: GetOrganizationEMAIssuerKeys :one
SELECT isi.issuer, COALESCE(jsonb_agg(k.public_jwk ORDER BY k.id) FILTER (WHERE k.id IS NOT NULL), '[]'::jsonb)::jsonb AS keys
FROM internal_session_issuers AS isi
JOIN organization_metadata AS o ON o.id = isi.organization_id AND o.disabled_at IS NULL
JOIN json_web_key_sets AS s
  ON s.organization_id = isi.organization_id
 AND s.id = isi.json_web_key_set_id
 AND s.deleted IS FALSE
LEFT JOIN json_web_keys AS k
  ON k.organization_id = s.organization_id
 AND k.json_web_key_set_id = s.id
 AND k.deleted IS FALSE
 AND (k.state IN ('pending', 'active')
   OR (k.state = 'retired' AND k.retired_at > clock_timestamp() - make_interval(secs => @retired_overlap_seconds::integer)))
WHERE isi.organization_id = @organization_id::text
  AND isi.deleted IS FALSE
GROUP BY isi.id, isi.issuer;

-- A cheap fingerprint of the issuer's key rows, revoked ones included, so a
-- cached JWKS notices any key write without reading the keys. Every row's id,
-- state, deletion and updated_at feed the digest, so a change to any row
-- changes it. A disabled organization has no fingerprint, as it has no keys.
-- name: GetOrganizationEMAIssuerKeysVersion :one
SELECT isi.json_web_key_set_id,
  md5(COALESCE(string_agg(k.id::text || ':' || k.state || ':' || k.deleted::text || ':' || k.updated_at::text, ',' ORDER BY k.id), ''))::text AS keys_digest
FROM internal_session_issuers AS isi
JOIN organization_metadata AS o ON o.id = isi.organization_id AND o.disabled_at IS NULL
LEFT JOIN json_web_keys AS k
  ON k.organization_id = isi.organization_id
 AND k.json_web_key_set_id = isi.json_web_key_set_id
WHERE isi.organization_id = @organization_id::text
  AND isi.deleted IS FALSE
GROUP BY isi.id, isi.json_web_key_set_id;

-- Retired issuer keys stop being published once nothing they signed can still
-- verify. Uses the database clock.
-- name: RevokeExpiredEMAIssuerKeys :many
UPDATE json_web_keys
SET state = 'revoked',
    revoked_at = clock_timestamp(),
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE json_web_key_set_id = @json_web_key_set_id
  AND organization_id = @organization_id
  AND state = 'retired'
  AND deleted IS FALSE
  AND retired_at <= clock_timestamp() - make_interval(secs => @overlap_seconds::integer)
RETURNING *;

-- Cheap check before the sweep takes the issuer lock.
-- name: HasExpiredEMAIssuerKeys :one
SELECT EXISTS (
  SELECT 1
  FROM internal_session_issuers AS isi
  JOIN json_web_keys AS k
    ON k.organization_id = isi.organization_id
   AND k.json_web_key_set_id = isi.json_web_key_set_id
  WHERE isi.organization_id = @organization_id
    AND isi.deleted IS FALSE
    AND k.state = 'retired'
    AND k.deleted IS FALSE
    AND k.retired_at <= clock_timestamp() - make_interval(secs => @overlap_seconds::integer)
);

-- System sweep: organizations whose live issuer has retired keys past the
-- overlap, revoked keys from any issuer row whose KMS cleanup is still
-- pending, or an enabled issuer with a pending key past the activation delay. Paged by organization id so every organization is reached each
-- run. Returns only organization ids; each is then swept org-qualified.
-- name: ListOrganizationsWithPendingEMAIssuerWork :many
SELECT DISTINCT isi.organization_id
FROM internal_session_issuers AS isi
JOIN json_web_keys AS k
  ON k.organization_id = isi.organization_id
 AND k.json_web_key_set_id = isi.json_web_key_set_id
WHERE isi.organization_id > @after_organization_id::text
  AND ((isi.deleted IS FALSE
      AND k.state = 'retired'
      AND k.deleted IS FALSE
      AND k.retired_at <= clock_timestamp() - make_interval(secs => @overlap_seconds::integer))
    OR (k.state = 'revoked' AND k.kms_disabled_at IS NULL)
    OR (isi.deleted IS FALSE
      AND isi.disabled_at IS NULL
      AND EXISTS (SELECT 1 FROM organization_metadata AS o WHERE o.id = isi.organization_id AND o.disabled_at IS NULL)
      AND k.state = 'pending'
      AND k.deleted IS FALSE
      -- 'infinity' is a publication that committed but was never observed;
      -- the sweep observes it and activates it a full delay later.
      AND (k.updated_at = 'infinity' OR k.updated_at <= clock_timestamp() - make_interval(secs => @activation_seconds::integer))))
ORDER BY isi.organization_id
LIMIT @max_organizations;

-- System metric: revoked issuer keys still awaiting KMS cleanup, fleet-wide.
-- Returns a count only.
-- name: CountPendingEMAIssuerKMSCleanup :one
SELECT count(*)::bigint
FROM internal_session_issuers AS isi
JOIN json_web_keys AS k
  ON k.organization_id = isi.organization_id
 AND k.json_web_key_set_id = isi.json_web_key_set_id
WHERE k.state = 'revoked'
  AND k.kms_disabled_at IS NULL;

-- Revoked issuer keys whose KMS version is not yet confirmed disabled, from
-- any issuer row the organization ever had. Least recently attempted first:
-- a failed attempt bumps updated_at, so keys that keep failing can't hold
-- back the rest.
-- name: ListPendingEMAIssuerKMSCleanup :many
SELECT k.*
FROM internal_session_issuers AS isi
JOIN json_web_keys AS k
  ON k.organization_id = isi.organization_id
 AND k.json_web_key_set_id = isi.json_web_key_set_id
WHERE isi.organization_id = @organization_id
  AND k.state = 'revoked'
  AND k.kms_disabled_at IS NULL
ORDER BY k.updated_at, k.id;

-- Records a failed KMS cleanup attempt so the next run tries other keys first.
-- name: TouchJsonWebKeyKMSCleanupAttempt :exec
UPDATE json_web_keys
SET updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND state = 'revoked'
  AND kms_disabled_at IS NULL;

-- Records that a revoked issuer key's KMS version is disabled and its signer
-- grant removed.
-- name: MarkJsonWebKeyKMSDisabled :exec
UPDATE json_web_keys
SET kms_disabled_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND state = 'revoked'
  AND kms_disabled_at IS NULL;

-- name: GetOrganizationInternalSessionIssuerURL :one
SELECT isi.issuer
FROM internal_session_issuers AS isi
JOIN organization_metadata AS o ON o.id = isi.organization_id AND o.disabled_at IS NULL
WHERE isi.organization_id = @organization_id
  AND isi.deleted IS FALSE;

-- Cleanup must verify deployment ownership even after an issuer is tombstoned
-- or its organization disabled. Such rows may still own KMS cleanup work.
-- name: GetInternalSessionIssuerForOwnership :one
SELECT issuer
FROM internal_session_issuers
WHERE organization_id = @organization_id
  AND json_web_key_set_id = @json_web_key_set_id;
