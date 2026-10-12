-- Modify "json_web_keys" table
ALTER TABLE "json_web_keys" ADD COLUMN "kms_disabled_at" timestamptz NULL;
-- Create "internal_session_issuers" table
CREATE TABLE "internal_session_issuers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "issuer" text NOT NULL,
  "json_web_key_set_id" uuid NOT NULL,
  "disabled_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "internal_session_issuers_key_set_tenant_fkey" FOREIGN KEY ("organization_id", "json_web_key_set_id") REFERENCES "json_web_key_sets" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "internal_session_issuers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "internal_session_issuers_issuer_key" to table: "internal_session_issuers"
CREATE UNIQUE INDEX "internal_session_issuers_issuer_key" ON "internal_session_issuers" ("issuer") WHERE (deleted IS FALSE);
-- Create index "internal_session_issuers_key_set_idx" to table: "internal_session_issuers"
CREATE INDEX "internal_session_issuers_key_set_idx" ON "internal_session_issuers" ("organization_id", "json_web_key_set_id");
-- Create index "internal_session_issuers_organization_id_id_key" to table: "internal_session_issuers"
CREATE UNIQUE INDEX "internal_session_issuers_organization_id_id_key" ON "internal_session_issuers" ("organization_id", "id");
-- Create index "internal_session_issuers_organization_id_key" to table: "internal_session_issuers"
CREATE UNIQUE INDEX "internal_session_issuers_organization_id_key" ON "internal_session_issuers" ("organization_id") WHERE (deleted IS FALSE);
-- Create "is_internal_session_issuer_external_key" function
CREATE FUNCTION "is_internal_session_issuer_external_key" ("org_id" text, "key_id" uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE AS $$
SELECT EXISTS (
    SELECT 1
    FROM internal_session_issuers AS isi
    JOIN json_web_key_sets AS s
      ON s.organization_id = isi.organization_id
     AND s.id = isi.json_web_key_set_id
    WHERE isi.organization_id = org_id
      AND (s.external_key_id = key_id OR EXISTS (
        SELECT 1 FROM json_web_keys AS k
        WHERE k.organization_id = s.organization_id
          AND k.json_web_key_set_id = s.id
          AND k.external_key_id = key_id
      ))
  );
$$;
-- Create "is_internal_session_issuer_key_set" function
CREATE FUNCTION "is_internal_session_issuer_key_set" ("org_id" text, "set_id" uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE AS $$
SELECT EXISTS (
    SELECT 1 FROM internal_session_issuers AS isi
    WHERE isi.organization_id = org_id
      AND isi.json_web_key_set_id = set_id
  );
$$;
-- Create "remote_session_ema_providers" table
CREATE TABLE "remote_session_ema_providers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "vendor_issuer" text NOT NULL,
  "remote_session_issuer_id" uuid NULL,
  "internal_session_issuer_id" uuid NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_ema_providers_internal_issuer_tenant_fkey" FOREIGN KEY ("organization_id", "internal_session_issuer_id") REFERENCES "internal_session_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "remote_session_ema_providers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_session_ema_providers_remote_issuer_tenant_fkey" FOREIGN KEY ("organization_id", "remote_session_issuer_id") REFERENCES "remote_session_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "remote_session_ema_providers_one_provider_check" CHECK (num_nonnulls(remote_session_issuer_id, internal_session_issuer_id) = 1)
);
-- Create index "remote_session_ema_providers_internal_issuer_idx" to table: "remote_session_ema_providers"
CREATE INDEX "remote_session_ema_providers_internal_issuer_idx" ON "remote_session_ema_providers" ("organization_id", "internal_session_issuer_id") WHERE (internal_session_issuer_id IS NOT NULL);
-- Create index "remote_session_ema_providers_position_key" to table: "remote_session_ema_providers"
CREATE UNIQUE INDEX "remote_session_ema_providers_position_key" ON "remote_session_ema_providers" ("organization_id", "vendor_issuer", "position") WHERE (deleted IS FALSE);
-- Create index "remote_session_ema_providers_provider_key" to table: "remote_session_ema_providers"
CREATE UNIQUE INDEX "remote_session_ema_providers_provider_key" ON "remote_session_ema_providers" ("organization_id", "vendor_issuer", "remote_session_issuer_id", "internal_session_issuer_id") NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Create index "remote_session_ema_providers_remote_issuer_idx" to table: "remote_session_ema_providers"
CREATE INDEX "remote_session_ema_providers_remote_issuer_idx" ON "remote_session_ema_providers" ("organization_id", "remote_session_issuer_id") WHERE (remote_session_issuer_id IS NOT NULL);
