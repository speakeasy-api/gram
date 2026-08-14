#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Lint sqlc queries to ensure they are bounded to a tenant (IDOR mitigation)"

#USAGE flag "--write-ignore-file" help="Rewrite .sqlclintignore from the current violations instead of failing. Commit the result; never hand-edit the file."
#USAGE flag "--schema-file <schema-file>" help="Schema that defines the tenancy shape. Defaults to the value in sqlclint.yaml."
#USAGE flag "--ignore-file <ignore-file>" help="Ignore file to read or write. Defaults to the value in sqlclint.yaml."

set -e

args=()

if [ "${usage_write_ignore_file:-}" = "true" ]; then
  args+=(--write-ignore-file)
fi

if [ -n "${usage_schema_file:-}" ]; then
  args+=(--schema-file "$usage_schema_file")
fi

if [ -n "${usage_ignore_file:-}" ]; then
  args+=(--ignore-file "$usage_ignore_file")
fi

# Remaining configuration (include/exclude globs, schema and ignore paths) comes
# from sqlclint.yaml at the repository root.
exec go run ./sqlclint run "${args[@]}"
