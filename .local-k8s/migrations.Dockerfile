# Bundles the atlas + golang-migrate CLIs with the repo's migration files so
# in-cluster Jobs can apply schema. Build context = a temp staging dir created by
# build-and-load.sh (server/.dockerignore would otherwise exclude these files).
FROM migrate/migrate:v4.18.1 AS migrate

FROM arigaio/atlas:latest-alpine
COPY --from=migrate /usr/local/bin/migrate /usr/local/bin/migrate
WORKDIR /work
COPY atlas.hcl ./atlas.hcl
# Postgres versioned migrations (atlas engine).
COPY migrations ./migrations
# ClickHouse migrations in golang-migrate up/down format (staged flat).
COPY golang_migrate ./clickhouse/golang_migrate
# Drop the atlas entrypoint so Job command:/args run the tool we choose.
ENTRYPOINT ["/bin/sh", "-c"]
