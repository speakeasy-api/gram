-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD CONSTRAINT "risk_policies_policy_type_check" CHECK (policy_type = ANY (ARRAY['standard'::text, 'prompt_based'::text])), ADD COLUMN "policy_type" text NOT NULL DEFAULT 'standard', ADD COLUMN "prompt" text NULL, ADD COLUMN "model_config" jsonb NULL;
