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
  "target_default_role_slug": "<TARGET_DEFAULT_ROLE_SLUG>",
  "assignments": [
    {
      "workos_directory_group_id": "<WORKOS_GROUP_ID>",
      "role_slug": "<ROLE_SLUG>"
    }
  ]
}
```

Several assignments may share a group ID. Duplicate assignments are harmless.
The default role is recorded but is neither imported nor changed. The optional
`target_default_role_slug` is the default you plan to set at cutover; the shadow
report uses it to flag members whose default would change, and import ignores
it. An empty assignment list is valid and does not clear mappings.

Configure `GRAM_DATABASE_URL`, `GRAM_REDIS_CACHE_ADDR`, and, if needed,
`GRAM_REDIS_CACHE_PASSWORD` for the same environment as the Gram server. For a
direct remote Redis connection, use `GRAM_DIRECTORY_ROLES_REDIS_URL` with a
`rediss://` URL instead of the address/password variables; it must verify the
server certificate. Direct remote PostgreSQL connections require
`sslmode=verify-full` and may not fall back to plaintext. Plaintext is accepted
only for literal loopback addresses, `localhost`, or Unix sockets. For remote
services reached through those local endpoints, use an authorised encrypted
tunnel or authenticated database proxy, never an unprotected forwarding route.
Supply
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
fails without committing any mappings if any group or role is absent, deleted or
ambiguous, or if a group or organisation-specific role belongs to another
organisation. Live built-in (global) roles are valid targets. Missing roles are
reported, never created. After inventoried roles and groups resolve, any preserved mapping that
points to a missing or deleted role blocks import and is listed in
`stale_mappings`. If that role is itself inventoried, the earlier
`missing_role_slugs` report blocks import first. Resolve the stale mapping through
the ordinary administrator workflow and repeat the preview; the importer never
silently removes it. Existing mappings are preserved, new mappings are added through the same
transactional set writer as the dashboard, and each addition is audited to the
staff operator. A repeated import adds no rows or audit entries. Every source
lock is acquired in stable order; any write or audit failure rolls back the
whole organisation's import. `committed` is true only after a successful commit.
A failed report write does not undo an already committed import; inspect live
state before retrying.

The shadow report calls the runtime `ListUserRolePrincipals` query. It compares
its normal result with the result excluding direct rows whose role matches an
inventoried rule, while retaining directory-mapped and other direct roles.

A direct role row does not record where it came from. The same row can be the
WorkOS group rule, an explicit assignment made in WorkOS, or the WorkOS default
role. The report cannot tell these apart, so it classifies what it can see and
leaves the decision to an administrator:

- `lost_role_urns`: an inventoried role held directly and not reached through
  any mapping. If the direct row was an independent assignment, cutover does not
  remove it; if it came from the group rule, the member loses it. Confirm which.
- `covered_role_urns`: an inventoried role held directly and also reached
  through a mapping. Access is kept either way, but an independent assignment
  will now follow directory group membership instead.
- `default_role_change`: set when `target_default_role_slug` differs from the
  current default and the member holds the current default directly. WorkOS may
  move them to the target default when it recomputes the membership; an explicit
  assignment of the same role would survive.
- `gained_role_urns`: always empty today, because the simulation only removes a
  role channel. Newly widened directory-mapped access must be checked separately.

Every listed member needs an administrator decision. Do not infer provenance or
approve a cutover from current group membership alone, and re-run the report
after cutover to confirm the result. The report does not predict WorkOS
recomputation timing or future events. Missing, ambiguous or tombstoned
inventoried sources and default roles block the report rather than silently
giving an incomplete result.

## Cutover checklist, operator-owned

**Stop here until both prerequisites are cleared:** a complete per-organisation
inventory (explicit rules and default role), and confirmation from WorkOS of when
removing a rule recomputes membership roles. The code does not clear these gates.

- [ ] Import is committed, and every intended rule is present in Gram.
- [ ] Shadow report (with `target_default_role_slug` set) is empty, or an administrator explicitly approved each difference.
- [ ] Review the least-privilege built-in default and its permissions separately.
- [ ] Remove explicit group-to-role assignments in the WorkOS Dashboard.
- [ ] Set that organisation's default role to the agreed least-privilege built-in role.
- [ ] Hide Admin Portal role assignment in WorkOS's environment-level Admin Portal settings; inspect the organisation Roles tab for an override that re-enables it.
- [ ] Confirm the environment default hides the step for new organisations and audit existing overrides.
- [ ] Observe subsequent membership events and confirm effective access in Gram.
- [ ] Re-run the shadow report and confirm no unexpected differences remain.

The portal setting is owned by WorkOS. Gram's portal-link API has no verified
visibility parameter, so this change adds no Gram toggle. Its onboarding default
must be configured in WorkOS by the environment owner, not by running this tool.

Do not delete direct assignments in Gram as part of cutover. The existing
membership sync replaces them when WorkOS emits the recomputed membership roles.
Administrator-assigned direct roles and the default-role channel remain supported.
If WorkOS emits no immediate recomputation, retain the imported mappings and
follow the confirmed WorkOS process rather than guessing at which rows to delete.
