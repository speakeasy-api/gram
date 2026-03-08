-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "rate_limit_rpm" integer NULL;
-- Create "platform_rate_limits" table
CREATE TABLE "platform_rate_limits" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "attribute_type" text NOT NULL,
  "attribute_value" text NOT NULL,
  "requests_per_minute" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_rate_limits_attribute_type_value_key" UNIQUE ("attribute_type", "attribute_value"),
  CONSTRAINT "platform_rate_limits_attribute_type_check" CHECK (attribute_type = ANY (ARRAY['mcp_slug'::text, 'project'::text, 'organization'::text])),
  CONSTRAINT "platform_rate_limits_requests_per_minute_check" CHECK (requests_per_minute > 0)
);
