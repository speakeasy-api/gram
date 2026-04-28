-- Soft-deprecate the resource column: make nullable, drop default, rename with
-- drop_ prefix to signal it is scheduled for removal.

ALTER TABLE principal_grants ALTER COLUMN resource DROP NOT NULL;
ALTER TABLE principal_grants ALTER COLUMN resource DROP DEFAULT;
ALTER TABLE principal_grants RENAME COLUMN resource TO drop_resource;
