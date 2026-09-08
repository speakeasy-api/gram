-- LOCAL-ONLY prerequisites for marts views, also used by CI and Atlas's
-- development database. This is not a ClickHouse Cloud provisioning template.
-- Terraform owns databases, users, roles, credentials, and role settings in Cloud.
-- Never copy their CREATE statements into application schema migrations.
-- Keep creation idempotent: local containers run bootstrap on every start.
-- Application migrations own the definer's source grant and reader view grants.
-- Omit an explicit database engine: Cloud does not support ENGINE = Atomic.
CREATE DATABASE IF NOT EXISTS marts;

CREATE ROLE IF NOT EXISTS marts_reader SETTINGS
    readonly = 1 CONST,
    max_execution_time = 30 CONST,
    max_memory_usage = 2000000000 CONST,
    max_rows_to_read = 100000000 CONST,
    max_bytes_to_read = 5000000000 CONST,
    max_threads = 4 CONST,
    max_result_rows = 10000 CONST,
    max_result_bytes = 10000000 CONST,
    result_overflow_mode = 'throw' CONST,
    max_concurrent_queries_for_user = 4 CONST;

-- LOCAL-ONLY passwordless principal: HOST NONE prevents login, but does not
-- exempt the user from Cloud's default password requirement. Cloud definers
-- need a Terraform-generated password kept out of logs, outputs, and this repo.
-- Never relax Cloud authentication policy to reproduce this local shortcut.
CREATE USER IF NOT EXISTS marts_definer HOST NONE;
