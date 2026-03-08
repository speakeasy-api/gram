-- name: GetPlatformRateLimit :one
-- Get a platform rate limit override by attribute type and value
SELECT * FROM platform_rate_limits
WHERE attribute_type = @attribute_type
AND attribute_value = @attribute_value;

-- name: UpsertPlatformRateLimit :one
-- Create or update a platform rate limit override
INSERT INTO platform_rate_limits (attribute_type, attribute_value, requests_per_minute)
VALUES (@attribute_type, @attribute_value, @requests_per_minute)
ON CONFLICT (attribute_type, attribute_value)
DO UPDATE SET
    requests_per_minute = @requests_per_minute,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeletePlatformRateLimit :exec
-- Delete a platform rate limit override
DELETE FROM platform_rate_limits
WHERE attribute_type = @attribute_type
AND attribute_value = @attribute_value;

-- name: ListPlatformRateLimits :many
-- List all platform rate limit overrides
SELECT * FROM platform_rate_limits
ORDER BY attribute_type, attribute_value;
