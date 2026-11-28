-- Local-only admin dashboard fixtures (runAdminSeed): fictional organizations,
-- members, and edge-case users. Never part of the tenant seed phases.

-- name: AdminSeedLock :exec
-- Serializes concurrent admin seed invocations for the transaction.
SELECT pg_advisory_xact_lock(890001);

-- name: LockAdminSeedIdentities :exec
-- Blocks other writers to the local-only fixture tables, including inserts
-- into absent reserved IDs, until the transaction ends.
LOCK TABLE organization_metadata, users,
  organization_user_relationships, trials,
  trial_payment_methods IN SHARE ROW EXCLUSIVE MODE;

-- name: AdminSeedOrganizationCollision :one
SELECT EXISTS (
 SELECT 1 FROM organization_metadata WHERE id= @organization_id::text
 AND (slug<> @slug::text OR workos_id IS NOT NULL)) AS collision;

-- name: AdminSeedMemberCollision :one
SELECT EXISTS (
 SELECT 1 FROM users WHERE id=ANY(@user_ids::text[])
 AND (email<>id||'@admin-seed.invalid' OR workos_id IS NOT NULL)
 UNION ALL
 SELECT 1 FROM organization_user_relationships
 WHERE user_id=ANY(@user_ids::text[]) AND organization_id<> @organization_id::text) AS collision;

-- name: AdminSeedUpsertOrganization :exec
INSERT INTO organization_metadata
   (id,name,slug,created_at,disabled_at) VALUES (@id::text,@name::text,@slug::text,@created_at::timestamptz,sqlc.narg('disabled_at')::timestamptz)
   ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,slug=EXCLUDED.slug,
   created_at=EXCLUDED.created_at,disabled_at=EXCLUDED.disabled_at,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertMember :exec
INSERT INTO users (id,email,display_name)
    VALUES (@id::text,@email::text,@display_name::text) ON CONFLICT (id) DO UPDATE SET
    email=EXCLUDED.email,display_name=EXCLUDED.display_name,deleted_at=NULL,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertMembership :exec
-- A reserved negative ID range avoids the normal identity sequence and
-- preserves membership IDs across reruns without delete/reinsert churn.
INSERT INTO organization_user_relationships
    (id,organization_id,user_id) VALUES (@id::bigint,@organization_id::text,@user_id::text)
    ON CONFLICT (organization_id,user_id) DO UPDATE SET deleted_at=NULL,updated_at= @updated_at::timestamptz;

-- name: AdminSeedEdgeUserCollision :one
SELECT EXISTS (
   SELECT 1 FROM users WHERE id= @user_id::text AND (email<>id||'@admin-seed.invalid' OR workos_id IS NOT NULL)
   UNION ALL SELECT 1 FROM organization_user_relationships WHERE user_id= @user_id::text AND NOT (organization_id=ANY(@organization_ids::text[]))) AS collision;

-- name: AdminSeedUpsertEdgeUser :exec
INSERT INTO users (id,email,display_name,deleted_at,workos_deleted_at,last_login)
   VALUES (@id::text,@email::text,@display_name::text,sqlc.narg('deleted_at')::timestamptz,sqlc.narg('workos_deleted_at')::timestamptz,sqlc.narg('last_login')::timestamptz) ON CONFLICT (id) DO UPDATE SET display_name=EXCLUDED.display_name,
   deleted_at=EXCLUDED.deleted_at,workos_deleted_at=EXCLUDED.workos_deleted_at,last_login=EXCLUDED.last_login,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertEdgeMembership :exec
INSERT INTO organization_user_relationships (id,organization_id,user_id,deleted_at)
    VALUES (@id::bigint,@organization_id::text,@user_id::text,sqlc.narg('deleted_at')::timestamptz) ON CONFLICT (organization_id,user_id) DO UPDATE SET deleted_at=EXCLUDED.deleted_at,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertTrialOrganization :exec
-- Free trial fixtures carry the billing state the staff trial views read, so
-- the upsert refreshes it on every run.
INSERT INTO organization_metadata
   (id,name,slug,gram_account_type,whitelisted,creation_source,created_at)
   VALUES (@id::text,@name::text,@slug::text,@gram_account_type::text,@whitelisted::boolean,@creation_source::text,@created_at::timestamptz)
   ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,slug=EXCLUDED.slug,
   gram_account_type=EXCLUDED.gram_account_type,whitelisted=EXCLUDED.whitelisted,
   creation_source=EXCLUDED.creation_source,created_at=EXCLUDED.created_at,
   disabled_at=NULL,updated_at= @updated_at::timestamptz;

-- name: AdminSeedClearTrial :exec
-- A fixture without a card that started its trial must have no trial
-- lifecycle, even after a local run started one. Scoped to the one fictional
-- organization.
DELETE FROM trials WHERE organization_id= @organization_id::text;

-- name: AdminSeedClearTrialPaymentMethod :exec
DELETE FROM trial_payment_methods WHERE organization_id= @organization_id::text;

-- name: AdminSeedUpsertTrial :exec
INSERT INTO trials (organization_id,tier,ends_at)
   VALUES (@organization_id::text,@tier::text,@ends_at::timestamptz)
   ON CONFLICT (organization_id) DO UPDATE SET tier=EXCLUDED.tier,ends_at=EXCLUDED.ends_at,
   converted_at=NULL,demoted_at=NULL,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertTrialPaymentMethod :exec
-- Fictional Stripe identifiers only. Two fixtures share a fingerprint on
-- purpose so the staff view shows a card offered by more than one
-- organization. A NULL rejection_reason means the card started the trial.
INSERT INTO trial_payment_methods
   (organization_id,stripe_customer_id,stripe_payment_method_id,card_fingerprint,card_funding,stripe_checkout_session_id,rejection_reason,created_at,updated_at)
   VALUES (@organization_id::text,@stripe_customer_id::text,@stripe_payment_method_id::text,@card_fingerprint::text,@card_funding::text,@stripe_checkout_session_id::text,sqlc.narg('rejection_reason')::text,@offered_at::timestamptz,@offered_at::timestamptz)
   ON CONFLICT (organization_id) DO UPDATE SET stripe_customer_id=EXCLUDED.stripe_customer_id,
   stripe_payment_method_id=EXCLUDED.stripe_payment_method_id,card_fingerprint=EXCLUDED.card_fingerprint,
   card_funding=EXCLUDED.card_funding,stripe_checkout_session_id=EXCLUDED.stripe_checkout_session_id,
   rejection_reason=EXCLUDED.rejection_reason,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at;
