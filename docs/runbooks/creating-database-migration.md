---
cwd: ../..
shell: bash
---

# Creating database migrations

We use [Atlas](https://atlasgo.io/) to create and apply database migrations declaratively. In other words, the overall process is:

1. We evolve the schema in [`server/database/schema.sql`](../../server/database/schema.sql)
2. Atlas works out what changed and generates a migration
3. On merge to main, the migration is applied to databases during the deployment pipeline

## Step 1. Evolve the schema

Decide what database changes you need to make and edit [`server/database/schema.sql`](../../server/database/schema.sql) to reflect the changes.

## Step 2. Generate a migration

Run the diff task to generate a migration:

```bash
echo "Enter a name for the migration (e.g. products-add-inventory-column): "
name=$(gum input --placeholder "migration name")
mise run db:diff "$name"
```

If atlas detects a difference then a migration file will be generated in the [`server/migrations/`](../../server/migrations/atlas.sum) directory.

**Optional:** You can lint the newly created migration file(s) to check for common footguns:

```bash
migpath=$(find server/migrations -type f -name "*.sql" | gum filter --header "Choose migrations to lint")
mise run lint:migrations --file "$migpath"
```

If something looks wrong at this point, you can always revert the migration with git and start again.

Next apply the pending migration(s) to your local database:

```bash
mise run db:migrate
```

## Step 3. PR your changes

Once you're happy with your changes, create a pull request _and get a thorough review_. In particular, pay close attention to migration linting issues posted on the PR.

> [!WARNING]
>
> A bad migration can destroy data or lock the entire database prevent running services from working.

If all seems well, you can merge your PR and the migration will be applied to the database during the deployment pipeline.
