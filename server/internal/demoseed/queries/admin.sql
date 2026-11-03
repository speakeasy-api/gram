-- Local-only admin dashboard fixtures (runAdminSeed): fictional organizations,
-- members, and edge-case users. Never part of the tenant seed phases.

-- name: AdminSeedLock :exec
-- Serializes concurrent admin seed invocations for the transaction.
SELECT pg_advisory_xact_lock(890001);

-- name: AdminSeedOrganizationCollision :one
SELECT EXISTS (
 SELECT 1 FROM public.organization_metadata WHERE id= @organization_id::text
 AND (slug<> @slug::text OR workos_id IS NOT NULL)) AS collision;

-- name: AdminSeedMemberCollision :one
SELECT EXISTS (
 SELECT 1 FROM public.users WHERE id=ANY(@user_ids::text[])
 AND (email<>id||'@admin-seed.invalid' OR workos_id IS NOT NULL)
 UNION ALL
 SELECT 1 FROM public.organization_user_relationships
 WHERE user_id=ANY(@user_ids::text[]) AND organization_id<> @organization_id::text) AS collision;

-- name: AdminSeedUpsertOrganization :exec
INSERT INTO public.organization_metadata
   (id,name,slug,created_at,disabled_at) VALUES (@id::text,@name::text,@slug::text,@created_at::timestamptz,sqlc.narg('disabled_at')::timestamptz)
   ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,slug=EXCLUDED.slug,
   created_at=EXCLUDED.created_at,disabled_at=EXCLUDED.disabled_at,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertMember :exec
INSERT INTO public.users (id,email,display_name)
    VALUES (@id::text,@email::text,@display_name::text) ON CONFLICT (id) DO UPDATE SET
    email=EXCLUDED.email,display_name=EXCLUDED.display_name,deleted_at=NULL,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertMembership :exec
-- A reserved negative ID range avoids the normal identity sequence and
-- preserves membership IDs across reruns without delete/reinsert churn.
INSERT INTO public.organization_user_relationships
    (id,organization_id,user_id) VALUES (@id::bigint,@organization_id::text,@user_id::text)
    ON CONFLICT (organization_id,user_id) DO UPDATE SET deleted_at=NULL,updated_at= @updated_at::timestamptz;

-- name: AdminSeedEdgeUserCollision :one
SELECT EXISTS (
   SELECT 1 FROM public.users WHERE id= @user_id::text AND (email<>id||'@admin-seed.invalid' OR workos_id IS NOT NULL)
   UNION ALL SELECT 1 FROM public.organization_user_relationships WHERE user_id= @user_id::text AND NOT (organization_id=ANY(@organization_ids::text[]))) AS collision;

-- name: AdminSeedUpsertEdgeUser :exec
INSERT INTO public.users (id,email,display_name,deleted_at,workos_deleted_at,last_login)
   VALUES (@id::text,@email::text,@display_name::text,sqlc.narg('deleted_at')::timestamptz,sqlc.narg('workos_deleted_at')::timestamptz,sqlc.narg('last_login')::timestamptz) ON CONFLICT (id) DO UPDATE SET display_name=EXCLUDED.display_name,
   deleted_at=EXCLUDED.deleted_at,workos_deleted_at=EXCLUDED.workos_deleted_at,last_login=EXCLUDED.last_login,updated_at= @updated_at::timestamptz;

-- name: AdminSeedUpsertEdgeMembership :exec
INSERT INTO public.organization_user_relationships (id,organization_id,user_id,deleted_at)
    VALUES (@id::bigint,@organization_id::text,@user_id::text,sqlc.narg('deleted_at')::timestamptz) ON CONFLICT (organization_id,user_id) DO UPDATE SET deleted_at=EXCLUDED.deleted_at,updated_at= @updated_at::timestamptz;
