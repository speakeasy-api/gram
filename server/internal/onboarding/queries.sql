-- Reference data. These tables are global; the server upserts them from its
-- Go catalog at startup, keyed by slug.

-- name: UpsertOnboardingProvider :one
INSERT INTO onboarding_providers (slug, name, sort_order)
VALUES (@slug, @name, @sort_order)
ON CONFLICT (slug) DO UPDATE SET
  name = EXCLUDED.name,
  sort_order = EXCLUDED.sort_order,
  updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertOnboardingPlan :one
INSERT INTO onboarding_plans (provider_id, slug, name, sort_order)
VALUES (@provider_id, @slug, @name, @sort_order)
ON CONFLICT (slug) DO UPDATE SET
  provider_id = EXCLUDED.provider_id,
  name = EXCLUDED.name,
  sort_order = EXCLUDED.sort_order,
  updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertOnboardingProduct :one
INSERT INTO onboarding_products (provider_id, slug, name, source_ids, sort_order)
VALUES (@provider_id, @slug, @name, @source_ids, @sort_order)
ON CONFLICT (slug) DO UPDATE SET
  provider_id = EXCLUDED.provider_id,
  name = EXCLUDED.name,
  source_ids = EXCLUDED.source_ids,
  sort_order = EXCLUDED.sort_order,
  updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertOnboardingTechnique :one
INSERT INTO onboarding_techniques (slug, name, description, sort_order)
VALUES (@slug, @name, @description, @sort_order)
ON CONFLICT (slug) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  sort_order = EXCLUDED.sort_order,
  updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertOnboardingCapability :one
INSERT INTO onboarding_capabilities (slug, name, use_case, sort_order)
VALUES (@slug, @name, @use_case, @sort_order)
ON CONFLICT (slug) DO UPDATE SET
  name = EXCLUDED.name,
  use_case = EXCLUDED.use_case,
  sort_order = EXCLUDED.sort_order,
  updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteOnboardingTechniqueCapabilities :exec
DELETE FROM onboarding_technique_capabilities WHERE technique_id = @technique_id;

-- name: InsertOnboardingTechniqueCapability :exec
INSERT INTO onboarding_technique_capabilities (technique_id, capability_id)
VALUES (@technique_id, @capability_id)
ON CONFLICT DO NOTHING;

-- name: DeleteOnboardingProductTechniques :exec
DELETE FROM onboarding_product_techniques WHERE product_id = @product_id;

-- name: InsertOnboardingProductTechnique :exec
INSERT INTO onboarding_product_techniques (product_id, technique_id)
VALUES (@product_id, @technique_id)
ON CONFLICT DO NOTHING;

-- name: InsertOnboardingProductTechniquePlan :exec
INSERT INTO onboarding_product_technique_plans (product_id, technique_id, plan_id)
VALUES (@product_id, @technique_id, @plan_id)
ON CONFLICT DO NOTHING;

-- name: ListOnboardingProviders :many
SELECT * FROM onboarding_providers ORDER BY sort_order, slug;

-- name: ListOnboardingPlans :many
SELECT pl.*, pr.slug AS provider_slug
FROM onboarding_plans pl
JOIN onboarding_providers pr ON pr.id = pl.provider_id
ORDER BY pl.sort_order, pl.slug;

-- name: ListOnboardingProducts :many
SELECT p.*, pr.slug AS provider_slug
FROM onboarding_products p
JOIN onboarding_providers pr ON pr.id = p.provider_id
ORDER BY p.sort_order, p.slug;

-- name: ListOnboardingTechniques :many
SELECT * FROM onboarding_techniques ORDER BY sort_order, slug;

-- name: ListOnboardingCapabilities :many
SELECT * FROM onboarding_capabilities ORDER BY sort_order, slug;

-- Answers. Organization scoped: onboarding is an organization-level activity
-- with no project of its own.

-- name: GetOnboardingAnswers :one
SELECT * FROM organization_onboarding_answers WHERE organization_id = @organization_id;

-- name: UpsertOnboardingStack :one
INSERT INTO organization_onboarding_answers (organization_id, mdm_vendor)
VALUES (@organization_id, @mdm_vendor)
ON CONFLICT (organization_id) DO UPDATE SET
  mdm_vendor = EXCLUDED.mdm_vendor,
  updated_at = clock_timestamp()
RETURNING *;

-- name: SetOnboardingUseCase :one
UPDATE organization_onboarding_answers
SET use_case = @use_case, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
RETURNING *;

-- name: ClearOnboardingCompletion :exec
UPDATE organization_onboarding_answers
SET completed_at = NULL, updated_at = clock_timestamp()
WHERE organization_id = @organization_id;

-- name: MarkOnboardingCompleted :exec
UPDATE organization_onboarding_answers
SET completed_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND completed_at IS NULL;

-- name: DeleteOnboardingSelectedProviders :exec
DELETE FROM organization_onboarding_providers WHERE organization_id = @organization_id;

-- name: InsertOnboardingSelectedProvider :exec
INSERT INTO organization_onboarding_providers (organization_id, provider_id, plan_id)
VALUES (@organization_id, @provider_id, @plan_id);

-- name: ListOnboardingSelectedProviders :many
SELECT op.organization_id, pr.slug AS provider_slug, pl.slug AS plan_slug
FROM organization_onboarding_providers op
JOIN onboarding_providers pr ON pr.id = op.provider_id
LEFT JOIN onboarding_plans pl ON pl.id = op.plan_id
WHERE op.organization_id = @organization_id
ORDER BY pr.sort_order, pr.slug;

-- name: DeleteOnboardingSelectedProducts :exec
DELETE FROM organization_onboarding_products WHERE organization_id = @organization_id;

-- name: InsertOnboardingSelectedProduct :exec
INSERT INTO organization_onboarding_products (organization_id, product_id)
VALUES (@organization_id, @product_id);

-- name: ListOnboardingSelectedProducts :many
SELECT op.organization_id, p.slug AS product_slug
FROM organization_onboarding_products op
JOIN onboarding_products p ON p.id = op.product_id
WHERE op.organization_id = @organization_id
ORDER BY p.sort_order, p.slug;

-- name: ListOnboardingSteps :many
SELECT * FROM organization_onboarding_steps
WHERE organization_id = @organization_id
ORDER BY created_at, step_slug;

-- name: UpsertOnboardingStep :one
INSERT INTO organization_onboarding_steps (organization_id, step_slug)
VALUES (@organization_id, @step_slug)
ON CONFLICT (organization_id, step_slug) DO UPDATE SET updated_at = clock_timestamp()
RETURNING *;

-- name: MarkOnboardingStepVerified :one
UPDATE organization_onboarding_steps
SET verified_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND step_slug = @step_slug
RETURNING *;

-- name: DeleteOnboardingSteps :exec
DELETE FROM organization_onboarding_steps WHERE organization_id = @organization_id;

-- Evidence read from Postgres. Cross-table reads are scoped to the
-- organization because onboarding has no project of its own.

-- name: CountActiveRiskPolicies :one
SELECT count(*) FROM risk_policies
WHERE organization_id = @organization_id AND enabled IS TRUE AND deleted IS FALSE;

-- name: CountRiskFindingsSince :one
SELECT count(*) FROM risk_results
WHERE organization_id = @organization_id AND found IS TRUE AND created_at >= @since;

-- name: CountPluginAssignments :one
SELECT count(*) FROM plugin_assignments pa
JOIN plugins p ON p.id = pa.plugin_id
WHERE pa.organization_id = @organization_id AND p.deleted IS FALSE;

-- name: GetOrganizationForOnboarding :one
SELECT id, name, slug FROM organization_metadata WHERE id = @organization_id;
