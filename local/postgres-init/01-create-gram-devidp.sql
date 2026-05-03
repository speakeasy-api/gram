-- Creates the dev-idp's logical database inside the existing gram-db
-- container. Postgres' docker-entrypoint runs every *.sql file in
-- /docker-entrypoint-initdb.d/ on FIRST container start (when the data
-- volume is empty). Devs with an existing volume must either recreate it
-- (`docker compose down -v && mise infra:start`) or run
-- `CREATE DATABASE gram_devidp;` manually against gram-db.
--
-- See server/internal/devidp/database/schema.sql + idp-design.md §5.4.
CREATE DATABASE gram_devidp;
