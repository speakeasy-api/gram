-- 🚨
-- 🚨 READ ./RULES.md BEFORE EDITING THIS FILE
-- 🚨

create extension if not exists pgcrypto;

-- https://blog.daveallie.com/ulid-primary-keys/
create or replace function generate_ulid() returns uuid
    as $$
        select (lpad(to_hex(floor(extract(epoch from clock_timestamp()) * 1000)::bigint), 12, '0') || encode(gen_random_bytes(10), 'hex'))::uuid;
    $$ language sql;

create table if not exists organizations (
  id uuid not null default generate_ulid(),
  name text not null,
  slug text not null,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,
  
  constraint organizations_pkey primary key (id),
  constraint organizations_slug_key unique (slug)
);

create table if not exists workspaces (
  id uuid not null default generate_ulid(),
  name text not null,
  slug text not null,
  organization_id uuid not null,
  
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,
  
  constraint workspaces_pkey primary key (id),
  constraint workspaces_organization_id_slug_key unique (organization_id, slug),
  constraint workspaces_organization_id_fkey foreign key (organization_id) references organizations (id)
);

create table if not exists users (
  id uuid not null default generate_ulid(),
  email text not null,
  verification uuid not null default generate_ulid(),
  verified_at timestamptz,
  
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,

  constraint users_pkey primary key (id),
  constraint users_email_key unique (email)
);

create table if not exists memberships (
  id uuid not null default generate_ulid(),
  user_id uuid not null,
  organization_id uuid not null,
  role text not null,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,
  
  constraint memberships_pkey primary key (id),
  constraint memberships_organization_id_fkey foreign key (organization_id) references organizations (id),
  constraint memberships_user_id_fkey foreign key (user_id) references users (id),
  constraint memberships_user_id_organization_id_key unique (user_id, organization_id, deleted)
);

create table if not exists deployments (
  id uuid not null default generate_ulid(),
  user_id uuid not null,
  organization_id uuid not null,
  workspace_id uuid not null,
  external_id text not null check (external_id != '' and length(external_id) <= 80),
  external_url text check (external_url != '' and length(external_url) <= 150),
 
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,
  
  constraint deployments_pkey primary key (id),
  constraint deployments_user_id_fkey foreign key (user_id) references users (id),
  constraint deployments_organization_id_fkey foreign key (organization_id) references organizations (id),
  constraint deployments_workspace_id_fkey foreign key (workspace_id) references workspaces (id)
);