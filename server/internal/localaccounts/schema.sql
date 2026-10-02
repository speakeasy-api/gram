-- Opt-in local fixture only; not a production migration.
CREATE SCHEMA IF NOT EXISTS gram_local;

CREATE TABLE IF NOT EXISTS gram_local.account_profiles (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id text REFERENCES public.organization_metadata(id) ON DELETE SET NULL,
  profile text NOT NULL,
  anchor timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS account_profiles_organization_id_key
ON gram_local.account_profiles(organization_id);
