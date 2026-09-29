-- Disposable benchmark tables in the selected scratch database. Run with
-- clickhouse-client --multiquery < product_metrics_encoding.sql.
SET max_threads = 2, max_block_size = 8192, max_insert_block_size = 8192,
    output_format_json_named_tuples_as_objects = 0;
DROP TABLE IF EXISTS product_metrics_encoding_json;
DROP TABLE IF EXISTS product_metrics_encoding_tuples;
CREATE TABLE product_metrics_encoding_json (
    organization_id String, project_id UInt64, metric_name String, bucket DateTime,
    attributes String, value UInt64
) ENGINE = MergeTree ORDER BY (organization_id, project_id, metric_name, bucket, attributes);
CREATE TABLE product_metrics_encoding_tuples (
    organization_id String, project_id UInt64, metric_name String, bucket DateTime,
    attributes Array(Tuple(key String, type String, value String)), value UInt64
) ENGINE = MergeTree ORDER BY (organization_id, project_id, metric_name, bucket, attributes);

-- 90% of records belong to one synthetic tenant. Half dense (20 attributes),
-- half sparse (2). Repeated and unique series share the same input distribution.
INSERT INTO product_metrics_encoding_tuples
SELECT if(number % 10 = 0, concat('synthetic-', toString(number % 100)), 'synthetic-heavy'),
    toUInt64(1), 'gram.synthetic.measurements',
    toDateTime('2026-09-01 00:00:00') + toIntervalMinute(number % 43200),
    arrayMap(i -> tuple(concat('dimension.', toString(i)), 'STRING',
        concat('"', toString(if(number % 2 = 0, number % 100, number)), '"')),
        range(toUInt32(if(number % 2 = 0, 2, 20)))), toUInt64(1)
FROM numbers(1000000);
INSERT INTO product_metrics_encoding_json
SELECT organization_id, project_id, metric_name, bucket, toJSONString(attributes), value
FROM product_metrics_encoding_tuples;

SELECT 'tuple-24h', sum(value) FROM product_metrics_encoding_tuples
WHERE organization_id = 'synthetic-heavy' AND project_id = 1 AND metric_name = 'gram.synthetic.measurements'
    AND bucket >= '2026-09-01 00:00:00' AND bucket < '2026-09-02 00:00:00'
    AND has(attributes, tuple('dimension.0', 'STRING', '"2"'));
SELECT 'json-24h', sum(value) FROM product_metrics_encoding_json
WHERE organization_id = 'synthetic-heavy' AND project_id = 1 AND metric_name = 'gram.synthetic.measurements'
    AND bucket >= '2026-09-01 00:00:00' AND bucket < '2026-09-02 00:00:00'
    AND has(JSONExtract(attributes, 'Array(Tuple(String, String, String))'), tuple('dimension.0', 'STRING', '"2"'));
SELECT 'tuple-30d', sum(value) FROM product_metrics_encoding_tuples
WHERE organization_id = 'synthetic-heavy' AND project_id = 1 AND metric_name = 'gram.synthetic.measurements'
    AND bucket >= '2026-09-01 00:00:00' AND bucket < '2026-10-01 00:00:00'
    AND has(attributes, tuple('dimension.0', 'STRING', '"2"'));
SELECT 'json-30d', sum(value) FROM product_metrics_encoding_json
WHERE organization_id = 'synthetic-heavy' AND project_id = 1 AND metric_name = 'gram.synthetic.measurements'
    AND bucket >= '2026-09-01 00:00:00' AND bucket < '2026-10-01 00:00:00'
    AND has(JSONExtract(attributes, 'Array(Tuple(String, String, String))'), tuple('dimension.0', 'STRING', '"2"'));

EXPLAIN indexes = 1 SELECT sum(value) FROM product_metrics_encoding_tuples
WHERE organization_id = 'synthetic-heavy' AND project_id = 1 AND metric_name = 'gram.synthetic.measurements'
    AND bucket >= '2026-09-01 00:00:00' AND bucket < '2026-09-02 00:00:00';
SYSTEM FLUSH LOGS;
SELECT query, query_duration_ms, read_rows, read_bytes, memory_usage
FROM system.query_log WHERE type = 'QueryFinish' AND query LIKE 'SELECT %24h%' AND query NOT LIKE '%system.query_log%'
ORDER BY event_time DESC LIMIT 2 FORMAT JSONEachRow;
SELECT query, query_duration_ms, read_rows, read_bytes, memory_usage
FROM system.query_log WHERE type = 'QueryFinish' AND query LIKE 'SELECT %30d%' AND query NOT LIKE '%system.query_log%'
ORDER BY event_time DESC LIMIT 2 FORMAT JSONEachRow;
SELECT table, sum(rows), sum(bytes_on_disk), count() FROM system.parts
WHERE active AND database = currentDatabase() AND table LIKE 'product_metrics_encoding_%' GROUP BY table;
