# Schema design rules

## Change tracking

All tables should have `created_at` and `updated_at` columns:

```sql
create table if not exists example (
  -- ...
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now() on update now(),
  -- ...
);
```

## Always soft delete

A nullable `deleted_at` column may be added to tables to perform soft deletes:

```sql
create table if not exists example (
  -- ...
  deleted_at timestamptz,
  deleted boolean not null generated always as (deleted_at is not null) stored,
  -- ...
);
```

Deleting rows with `DELETE FROM table` is not strongly discouraged. Instead,
use:

```sql
UPDATE example SET deleted_at = now() WHERE id = ?;
```

## Constraint naming

All constraints should be named with this format:

```
{tablename}_{columnname(s)}_{suffix}
```

Where suffix is:

- `key` for a unique constraint
- `fkey` for a foreign key constraint
- `idx` for any other kind of index
- `check` for a check constraint
- `excl` for an exclusion constraint
- `seq` for an sequences
