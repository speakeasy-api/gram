-- Modify "network_ingresses" table
ALTER TABLE "network_ingresses" ADD COLUMN "runtime" text NULL, ADD COLUMN "gateway_shard" text NOT NULL DEFAULT 'shared';
-- Set comment to column: "runtime" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."runtime" IS '@access: confidential';
-- Set comment to column: "gateway_shard" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."gateway_shard" IS '@access: confidential';
-- Create "network_ingress_node_states" table
CREATE TABLE "network_ingress_node_states" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "network_ingress_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "slot" integer NOT NULL,
  "state_key" text NOT NULL,
  "value_encrypted" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "network_ingress_node_states_network_ingress_id_fkey" FOREIGN KEY ("network_ingress_id") REFERENCES "network_ingresses" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "network_ingress_node_states_state_key_check" CHECK (state_key <> ''::text)
);
-- Create index "network_ingress_node_states_network_ingress_id_slot_state_key_k" to table: "network_ingress_node_states"
CREATE UNIQUE INDEX "network_ingress_node_states_network_ingress_id_slot_state_key_k" ON "network_ingress_node_states" ("network_ingress_id", "slot", "state_key");
-- Set comment to column: "id" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."id" IS '@access: confidential';
-- Set comment to column: "network_ingress_id" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."network_ingress_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."organization_id" IS '@access: confidential';
-- Set comment to column: "slot" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."slot" IS '@access: confidential';
-- Set comment to column: "state_key" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."state_key" IS '@access: confidential';
-- Set comment to column: "value_encrypted" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."value_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "created_at" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."updated_at" IS '@access: confidential';
-- Create "network_ingress_nodes" table
CREATE TABLE "network_ingress_nodes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "network_ingress_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "slot" integer NOT NULL,
  "pod_name" text NULL,
  "auth_key_encrypted" text NULL,
  "auth_key_expires_at" timestamptz NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "node_id" text NULL,
  "dns_name" text NULL,
  "last_error" text NULL,
  "last_seen_at" timestamptz NULL,
  "connected_since" timestamptz NULL,
  "retired_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "network_ingress_nodes_network_ingress_id_fkey" FOREIGN KEY ("network_ingress_id") REFERENCES "network_ingresses" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "network_ingress_nodes_network_ingress_id_slot_key" to table: "network_ingress_nodes"
CREATE UNIQUE INDEX "network_ingress_nodes_network_ingress_id_slot_key" ON "network_ingress_nodes" ("network_ingress_id", "slot");
-- Create index "network_ingress_nodes_organization_id_idx" to table: "network_ingress_nodes"
CREATE INDEX "network_ingress_nodes_organization_id_idx" ON "network_ingress_nodes" ("organization_id");
-- Set comment to column: "id" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."id" IS '@access: confidential';
-- Set comment to column: "network_ingress_id" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."network_ingress_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."organization_id" IS '@access: confidential';
-- Set comment to column: "slot" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."slot" IS '@access: confidential';
-- Set comment to column: "pod_name" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."pod_name" IS '@access: confidential';
-- Set comment to column: "auth_key_encrypted" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."auth_key_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "auth_key_expires_at" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."auth_key_expires_at" IS '@access: confidential';
-- Set comment to column: "status" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."status" IS '@access: confidential';
-- Set comment to column: "node_id" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."node_id" IS '@access: restricted';
-- Set comment to column: "dns_name" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."dns_name" IS '@access: restricted';
-- Set comment to column: "last_error" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."last_error" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "connected_since" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."connected_since" IS '@access: confidential';
-- Set comment to column: "retired_at" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."retired_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."updated_at" IS '@access: confidential';
