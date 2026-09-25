-- name: ReadAccountState :one
SELECT gram_account_type, whitelisted,
 COALESCE((SELECT to_jsonb(t) FROM trials t WHERE t.organization_id=o.id),'null'::jsonb)::jsonb AS trial,
 ARRAY(SELECT feature_name FROM organization_features WHERE organization_id=o.id AND NOT deleted ORDER BY feature_name)::text[] AS features
 FROM organization_metadata o WHERE o.id=$1 AND disabled_at IS NULL;

-- name: AccountProfilesExist :one
SELECT (to_regclass('gram_local.account_profiles') IS NOT NULL)::boolean AS exists;

-- name: GetAccountProfile :one
SELECT profile,anchor FROM gram_local.account_profiles WHERE organization_id=$1;

-- name: LockAccountProfile :exec
SELECT pg_advisory_xact_lock(hashtextextended('gram-local-account-profile',0));

-- name: LockAccountMetadata :exec
SELECT id FROM organization_metadata WHERE id=$1 FOR UPDATE;

-- name: LockAccountTrial :exec
SELECT organization_id FROM trials WHERE organization_id=$1 FOR UPDATE;

-- name: SetAccountTier :exec
UPDATE organization_metadata SET gram_account_type=$2, whitelisted=$3, updated_at=clock_timestamp()
 WHERE id=$1 AND (gram_account_type IS DISTINCT FROM $2 OR whitelisted IS DISTINCT FROM $3);

-- name: ConvertAccountTrial :exec
UPDATE trials SET converted_at=$2, updated_at=clock_timestamp() WHERE organization_id=$1 AND converted_at IS NULL;

-- name: UpsertAccountTrial :exec
INSERT INTO trials(organization_id,tier,ends_at,converted_at,demoted_at,created_at,updated_at)
 VALUES($1,'enterprise',$2,NULL,$3,$4,$4)
 ON CONFLICT(organization_id) DO UPDATE SET tier='enterprise',ends_at=EXCLUDED.ends_at,converted_at=NULL,demoted_at=EXCLUDED.demoted_at,updated_at=EXCLUDED.updated_at
 WHERE (trials.tier,trials.ends_at,trials.converted_at,trials.demoted_at) IS DISTINCT FROM ('enterprise',EXCLUDED.ends_at,NULL::timestamptz,EXCLUDED.demoted_at);

-- name: UpsertAccountProfile :exec
INSERT INTO gram_local.account_profiles(organization_id,profile,anchor) VALUES($1,$2,$3)
 ON CONFLICT(organization_id) DO UPDATE SET profile=EXCLUDED.profile,anchor=EXCLUDED.anchor
 WHERE (account_profiles.profile,account_profiles.anchor) IS DISTINCT FROM (EXCLUDED.profile,EXCLUDED.anchor);

-- name: GetAccountProfileName :one
SELECT profile FROM gram_local.account_profiles WHERE organization_id=$1;

-- name: AccountTrialDue :one
SELECT ends_at <= clock_timestamp() AND converted_at IS NULL AND demoted_at IS NULL FROM trials WHERE organization_id=$1 FOR UPDATE;

-- name: ExpireAccountTier :exec
UPDATE organization_metadata SET gram_account_type='free',whitelisted=false WHERE id=$1;

-- name: DemoteAccountTrial :exec
UPDATE trials SET demoted_at=clock_timestamp() WHERE organization_id=$1;

-- name: ResolveAccountTargets :many
SELECT u.id, r.organization_id, u.workos_id, COALESCE(o.workos_id, '') AS workos_organization_id
 FROM users u
 JOIN organization_user_relationships r ON r.user_id = u.id AND r.deleted_at IS NULL
 LEFT JOIN organization_metadata o ON o.id = r.organization_id AND o.disabled_at IS NULL
 WHERE u.workos_id = $1 AND u.deleted_at IS NULL AND u.workos_deleted_at IS NULL
 LIMIT 2;

-- name: AccountTrialActive :one
SELECT ends_at > clock_timestamp() AND demoted_at IS NULL AND converted_at IS NULL
 FROM trials WHERE organization_id = $1;

-- name: TryLockAccountLifecycle :one
SELECT pg_try_advisory_lock(hashtextextended('gram-local-account-lifecycle',0));
