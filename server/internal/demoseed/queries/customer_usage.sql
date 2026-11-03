-- Local-only admin Customer usage fixtures (RunCustomerUsageSeed): fictional
-- organizations with billing anchors and trials. Never part of the tenant seed
-- phases.

-- name: CustomerUsageSeedLock :exec
-- Session-level: held on one dedicated connection across the Postgres
-- transaction and the ClickHouse replacement.
SELECT pg_advisory_lock(@lock_id::bigint);

-- name: CustomerUsageSeedUnlock :exec
SELECT pg_advisory_unlock(@lock_id::bigint);

-- name: CustomerUsageSeedOrganizationCollision :one
SELECT EXISTS (
 SELECT 1 FROM public.organization_metadata WHERE id= @organization_id::text
 AND (slug<> @slug::text OR workos_id IS NOT NULL)) AS collision;

-- name: CustomerUsageSeedDeleteTrials :exec
DELETE FROM public.trials WHERE organization_id=ANY(@organization_ids::text[]);

-- name: CustomerUsageSeedDeleteBillingMetadata :exec
DELETE FROM public.billing_metadata WHERE organization_id=ANY(@organization_ids::text[]);

-- name: CustomerUsageSeedUpsertOrganization :exec
INSERT INTO public.organization_metadata
   (id,name,slug,gram_account_type,created_at,disabled_at) VALUES (@id::text,@name::text,@slug::text,@gram_account_type::text,@created_at::timestamptz,NULL)
   ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,slug=EXCLUDED.slug,
   gram_account_type=EXCLUDED.gram_account_type,created_at=EXCLUDED.created_at,
   disabled_at=NULL,updated_at= @updated_at::timestamptz;

-- name: CustomerUsageSeedInsertBillingAnchor :exec
INSERT INTO public.billing_metadata (organization_id,billing_cycle_anchor_day)
   VALUES (@organization_id::text,@billing_cycle_anchor_day::int);

-- name: CustomerUsageSeedInsertTrial :exec
INSERT INTO public.trials (organization_id,tier,created_at,ends_at,converted_at,demoted_at)
   VALUES (@organization_id::text,'enterprise',@created_at::timestamptz,@ends_at::timestamptz,sqlc.narg('converted_at')::timestamptz,NULL);
