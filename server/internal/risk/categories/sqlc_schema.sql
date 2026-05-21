-- Schema fragment consumed ONLY by sqlc's static analysis so that queries in
-- internal/risk/queries.sql can reference the risk_category_lookup TEMP TABLE
-- without a migration.
--
-- The actual table is created at runtime by BootstrapConnection (bootstrap.go)
-- on every pool connection, populated from the canonical Go classifier.
-- DO NOT migrate this file; it is intentionally not in server/migrations.
CREATE TABLE IF NOT EXISTS risk_category_lookup (
    priority    INTEGER NOT NULL,
    category    TEXT    NOT NULL,
    source      TEXT,
    rule_id     TEXT,
    rule_prefix TEXT
);
