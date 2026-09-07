-- Local prerequisites for marts views. Terraform owns these objects in Cloud.
-- This bootstrap also runs in CI and the Atlas development database.
-- Keep creation idempotent: local containers run bootstrap on every start.
-- Application migrations own the definer's source grant and reader view grants.
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

-- HOST NONE prevents this definer principal from logging in.
CREATE USER IF NOT EXISTS marts_definer HOST NONE;
