-- Create "generate_uuidv7" function
create or replace function generate_uuidv7()
returns uuid
as $$
begin
  return encode(
    set_bit(
      set_bit(
        overlay(uuid_send(gen_random_uuid())
                placing substring(int8send(floor(extract(epoch from clock_timestamp()) * 1000)::bigint) from 3)
                from 1 for 6
        ),
        52, 1
      ),
      53, 1
    ),
    'hex')::uuid;
end
$$
language plpgsql
volatile;
-- Create "organizations" table
CREATE TABLE "organizations" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "name" text NOT NULL, "slug" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "deleted_at" timestamptz NULL, "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED, PRIMARY KEY ("id"), CONSTRAINT "organizations_slug_key" UNIQUE ("slug"));
-- Create "projects" table
CREATE TABLE "projects" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "organization_id" uuid NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "deleted_at" timestamptz NULL, "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED, PRIMARY KEY ("id"), CONSTRAINT "projects_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organizations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create "users" table
CREATE TABLE "users" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "email" text NOT NULL, "verification" uuid NOT NULL DEFAULT generate_uuidv7(), "verified_at" timestamptz NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "deleted_at" timestamptz NULL, "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED, PRIMARY KEY ("id"), CONSTRAINT "users_email_key" UNIQUE ("email"));
-- Create "deployments" table
CREATE TABLE "deployments" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "user_id" uuid NULL, "project_id" uuid NULL, "organization_id" uuid NULL, "manifest_version" text NOT NULL, "manifest_url" text NOT NULL, "github_repo" text NULL, "github_pr" text NULL, "external_id" text NULL, "external_url" text NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"), CONSTRAINT "deployments_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organizations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "deployments_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "deployments_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "deployments_external_id_check" CHECK ((external_id <> ''::text) AND (length(external_id) <= 80)), CONSTRAINT "deployments_external_url_check" CHECK ((external_url <> ''::text) AND (length(external_url) <= 150)), CONSTRAINT "deployments_github_pr_check" CHECK ((github_pr <> ''::text) AND (length(github_pr) <= 10)));
-- Create "deployment_logs" table
CREATE TABLE "deployment_logs" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "seq" bigserial NOT NULL, "event" text NOT NULL, "deployment_id" uuid NULL, "project_id" uuid NULL, "tooltemplate_id" uuid NULL, "tooltemplate_type" text NULL, "collection_id" uuid NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"), CONSTRAINT "deployment_logs_seq_key" UNIQUE ("seq"), CONSTRAINT "deployment_logs_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "deployment_logs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "deployment_logs_check" CHECK ((tooltemplate_id IS NULL) OR (tooltemplate_type IS NOT NULL)));
-- Create "deployment_statuses" table
CREATE TABLE "deployment_statuses" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "seq" bigserial NOT NULL, "deployment_id" uuid NULL, "status" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"), CONSTRAINT "deployment_statuses_seq_key" UNIQUE ("seq"), CONSTRAINT "deployment_statuses_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create "http_tool_definitions" table
CREATE TABLE "http_tool_definitions" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "organization_id" uuid NULL, "project_id" uuid NULL, "name" text NOT NULL, "description" text NOT NULL, "server_env_var" text NOT NULL, "security_type" text NOT NULL, "bearer_env_var" text NULL, "apikey_env_var" text NULL, "username_env_var" text NULL, "password_env_var" text NULL, "http_method" text NOT NULL, "path" text NOT NULL, "headers_schema" jsonb NULL, "queries_schema" jsonb NULL, "pathparams_schema" jsonb NULL, "body_schema" jsonb NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "deleted_at" timestamptz NULL, "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED, PRIMARY KEY ("id"), CONSTRAINT "http_tool_definitions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organizations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "http_tool_definitions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "http_tool_definitions_security_type_check" CHECK (security_type = ANY (ARRAY['http:bearer'::text, 'http:basic'::text, 'apikey'::text])));
-- Create "memberships" table
CREATE TABLE "memberships" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "user_id" uuid NULL, "organization_id" uuid NULL, "role" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "deleted_at" timestamptz NULL, "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED, PRIMARY KEY ("id"), CONSTRAINT "memberships_user_id_organization_id_key" UNIQUE ("user_id", "organization_id", "deleted"), CONSTRAINT "memberships_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organizations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "memberships_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
