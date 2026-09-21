-- atlas:txtar

-- checks/destructive.sql --
-- The legacy-policy-scope migration folded every live policy's value into
-- per-category detection scopes and validated an empty population in dev and
-- production. This assertion refuses the drop wherever that is not true, which
-- matters because the pre-fold values are no longer backed up anywhere.
--
-- It mirrors the fold's own candidate predicate rather than a plain NOT NULL
-- check: a stored empty array or empty string narrows nothing and was
-- deliberately left alone, and soft-deleted policies were never folded because
-- no scan path reads them. In production today 66 live rows hold an empty
-- message_types array and 10 soft-deleted rows still hold a real one, so a
-- NOT NULL assertion would refuse a drop that is in fact safe.
-- atlas:assert DS103
SELECT NOT EXISTS (
  SELECT 1 FROM "risk_policies"
  WHERE "deleted" IS FALSE
    AND (("message_types" IS NOT NULL AND cardinality("message_types") > 0)
         OR coalesce("scope_include", '') <> ''
         OR coalesce("scope_exempt", '') <> '')
) AS "is_empty";

-- migration.sql --
-- Modify "risk_policies" table
ALTER TABLE "risk_policies" DROP COLUMN "message_types", DROP COLUMN "scope_include", DROP COLUMN "scope_exempt";
