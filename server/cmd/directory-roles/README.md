# Retire WorkOS group-to-role rules safely

WorkOS group rules arrive in Gram as direct role assignments without provenance.
Import their rules into Gram's directory mappings before removing them in WorkOS.
The command creates no roles, changes no grants, and never calls WorkOS.

## Inventory and credentials

An authorised staff operator records the explicit rules and current default role
from the WorkOS Dashboard for one organisation. Use WorkOS directory group IDs,
not group names. There is no verified API for listing these rules.

Keep the inventory and reports outside the checkout in restricted local files.
Do not attach them to PRs or public comments. Every example below is a placeholder.

```json
{
  "organization_id": "<ORG_ID>",
  "workos_organization_id": "<WORKOS_ORG_ID>",
  "default_role_slug": "<DEFAULT_ROLE_SLUG>",
  "assignments": [
    {
      "workos_directory_group_id": "<WORKOS_GROUP_ID>",
      "role_slug": "<ROLE_SLUG>"
    }
  ]
}
```

Several assignments may share a group ID. Duplicate assignments are harmless.
The default role is recorded but is neither imported nor changed. An empty
assignment list is valid and does not clear mappings.

Configure `GRAM_DATABASE_URL`, `GRAM_REDIS_CACHE_ADDR`, and, if needed,
`GRAM_REDIS_CACHE_PASSWORD` for the same environment as the Gram server. Supply
`GRAM_SUPPORT_SESSION_TOKEN` securely through the environment, not command-line
flags, inventory files, shell history, or reports. Obtain a current, bounded
support session for the exact inventory organisation through the existing staff
support-session workflow. Ordinary sessions and API keys are not accepted.
The session is not refreshed by this command.

## Import, then check

The role-set API and removal of the source-only unique indexes must be deployed
before importing a group with more than one role. Applying only the additive
role-specific indexes is insufficient. A partial rollout fails safely and rolls
back the entire import, including audit entries.

These commands require privileged database and session-cache access. Run them
only in the authorised operator environment with credentials obtained through the
normal access process. Redis is a trusted authentication store, not an untrusted
input boundary: anyone who can alter it or the database already holds privileged
access. The session check validates the target and attributes the action, but
does not replace infrastructure access controls. Expiry is checked at command
startup; obtain a fresh bounded session before each action and keep invocations
short. The import also rechecks and locks the live staff entitlement and target
organisation in its transaction.

Run from the repository root with pinned tools. For development and automated
validation, use local fixtures and the local stack only.

```sh
# Default import mode performs the writes and audit in a rolled-back transaction.
mise exec -- go run ./server/cmd/directory-roles import < /private/path/inventory.json

# Commit only after reviewing the private preview and confirming the exact target.
mise exec -- go run ./server/cmd/directory-roles import -apply -confirm-organization '<ORG_ID>' < /private/path/inventory.json

# Read-only, consistent-snapshot evaluation of every live member.
mise exec -- go run ./server/cmd/directory-roles shadow < /private/path/inventory.json
```

Redirect stdout to a restricted private file when retaining a report. Import
fails without committing any mappings if any group or role is absent, deleted,
ambiguous, or outside the target organisation. Missing roles are reported, never
created. If an existing mapping points to a missing or deleted role, import fails
with the source and role listed in `stale_mappings`. Resolve that mapping through
the ordinary administrator workflow and repeat the preview; the importer never
silently removes it. Existing mappings are preserved, new roles are added through the same
transactional set writer as the dashboard, and each addition is audited to the
staff operator. A repeated import adds no rows or audit entries. Every source
lock is acquired in stable order; any write or audit failure rolls back the
whole organisation's import. `committed` is true only after a successful commit.
A failed report write does not undo an already committed import; inspect live
state before retrying.

The shadow report calls the runtime `ListUserRolePrincipals` query. It compares
its normal result with the result excluding direct rows whose role matches an
inventoried rule, while retaining directory-mapped and other direct roles.
Because direct rows have no provenance, this is deliberately conservative: it
also excludes independently assigned copies of an inventoried role, even for a
member no longer in that group. Review each difference with an administrator;
do not infer provenance or approve a cutover merely from current group membership.
The simulation only removes a role channel, so it cannot produce gains. It does
not predict WorkOS recomputation, changing the default role, or future events.
Check newly widened directory-mapped access and the intended default role
separately with the administrator. A missing/tombstoned inventoried source blocks
the report rather than silently giving an incomplete result.

## Cutover checklist, operator-owned

**Stop here until both prerequisites are cleared:** a complete per-organisation
inventory (explicit rules and default role), and confirmation from WorkOS of when
removing a rule recomputes membership roles. The code does not clear these gates.

- [ ] Import is committed, and every intended rule is present in Gram.
- [ ] Shadow report is empty, or an administrator explicitly approved each difference.
- [ ] Review the least-privilege built-in default and its permissions separately.
- [ ] Remove explicit group-to-role assignments in the WorkOS Dashboard.
- [ ] Set that organisation's default role to the agreed least-privilege built-in role.
- [ ] Hide Admin Portal role assignment in WorkOS's environment-level Admin Portal settings; inspect the organisation Roles tab for an override that re-enables it.
- [ ] Confirm the environment default hides the step for new organisations and audit existing overrides.
- [ ] Observe subsequent membership events and confirm effective access in Gram.

The portal setting is owned by WorkOS. Gram's portal-link API has no verified
visibility parameter, so this change adds no Gram toggle. Its onboarding default
must be configured in WorkOS by the environment owner, not by running this tool.

Do not delete direct assignments in Gram as part of cutover. The existing
membership sync replaces them when WorkOS emits the recomputed membership roles.
Administrator-assigned direct roles and the default-role channel remain supported.
If WorkOS emits no immediate recomputation, retain the imported mappings and
follow the confirmed WorkOS process rather than guessing at which rows to delete.
