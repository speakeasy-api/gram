INSERT INTO attribute_keys
SELECT
    gram_project_id,
    arrayJoin(JSONAllPaths(attributes)) AS attribute_key,
    min(time_unix_nano) AS first_seen_unix_nano,
    max(time_unix_nano) AS last_seen_unix_nano
FROM telemetry_logs
GROUP BY gram_project_id, attribute_key;
