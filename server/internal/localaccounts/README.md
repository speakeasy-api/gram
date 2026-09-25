# Local account profiles

Supported only in initialized **secondary Git worktrees**, not the primary
checkout. Create a secondary worktree and run `./zero` there before using these
commands. Its configuration must include an explicit Compose project and a
matching, non-default Temporal namespace; changing those values on the primary
checkout is not a supported workaround.

`mise run account` changes the selected local development account only. It is not
an admin/billing API and has no Platform MCP tool. It never wakes or seeds the
stack, switches identity, provisions users, or calls billing/email/key providers.

## Usage

```sh
mise run account status
mise run account repair --dry-run
mise run account repair
mise run account apply enterprise --dry-run
mise run account apply payg
mise run account apply active-trial
mise run account apply expired-trial
mise run account status --json
```

`repair` means `apply enterprise`. There is no user/org selector or Book a Demo
profile. The running local dev-idp's selected oauth2-1 user must already be linked
to a Gram user with exactly one active organization.

Output is a human summary; progress goes to stderr. Add `--json` for full state
and results on stdout, including `committed` on post-commit failures. Flags may
appear before or after the profile. Status shows both the requested fixture marker
and actual account/trial state, which can change independently.

## Profiles

- **Enterprise / repair:** enterprise tier, whitelisted, enterprise entitlements
  and trial runtime gates enabled. Pending trials are marked converted.
- **PAYG:** PAYG tier, whitelisted, pending trials converted. Never-trialled
  accounts receive only never-configured enterprise entitlements/runtime gates;
  explicit disables remain disabled. Existing trial entitlements are preserved
  and runtime gates restored. No role grants are created.
- **Active trial:** enterprise tier, whitelisted, enterprise entitlements and
  runtime gates enabled; unconverted/undemoted trial ending 14 days after its anchor.
- **Expired trial:** free tier, not whitelisted, trial ended one day before its
  anchor and marked demoted. Only trial runtime gates are disabled.

Dates anchor to UTC midnight. Reapplying a profile retains its anchor, reconciles
drift and refreshes caches without churning unchanged state. An active fixture
whose stored trial has expired or been demoted cannot be reapplied, even in a
dry-run: apply `enterprise`, then `active-trial` to explicitly restart it.

Projects, deployments, keys and their disable causes, roles, memberships, sessions,
users, and unrelated features are preserved. This is not blanket key reactivation:
a key disabled by a real trial demotion stays disabled.

## Safety and caveats

- This tooling assumes a trusted development machine. The existing dev IdP
  management API uses unauthenticated loopback HTTP, including identity selection.
  Provenance detects accidental worktree/database mismatches; it is not
  cryptographic authentication of the responding process. A malicious local
  process can forge that response or change the selection. Hostile local
  processes/users are outside this tool's safety boundary. Do not expose or
  forward the dev IdP management port to untrusted clients.
- Only this worktree's verified local database, Redis and Temporal are accepted.
  The selected-user response must include local-backend provenance matching this
  worktree's canonical root and the IdP's actual opened SQLite database. Missing
  provenance (including an older IdP), another worktree, or another database is refused.
  External billing configuration, ambiguous selection, unknown daemon origin/state,
  and unavailable safety checks fail closed.
- Status and dry-run never stop/start services. Dry-run validates and takes locks
  but writes no rows or caches; it is a point-in-time plan, not a reservation.
- Apply/repair stop this worktree's server and worker and restore only writers
  that were originally running, including after errors or SIGINT/SIGTERM.
  SIGKILL and crashes cannot guarantee recovery. Never-registered writers must
  first be started normally so Pitchfork can verify their worktree origin.
- Queued/retrying trial demotions block writes. The command never cancels workflows
  or pauses schedules. Do not restart writers, enqueue workflows or switch IdP
  selection during an apply. Overlapping writes are refused through recovery.
- Nonzero failures distinguish uncommitted changes from committed state with cache
  or restart errors. Check `committed` and per-writer progress before retrying;
  restore Redis for cache failures and use Pitchfork for restart failures.
- The hourly trial sweep projects elapsed active fixtures to Free/inactive without
  provider calls or key/role/resource changes. A stopped worker delays projection;
  use `expired-trial` for immediate expiry. The local billing stub checks the actual
  deadline/demotion too, so session reconciliation cannot resurrect expired trials.
  Unmarked accounts retain the original Pro/active stub behavior.
- Ordinary seeding is separate and can reset state/resources. Do not seed to repair
  a profile; rerun this tool and inspect `status`. No session flush is needed.
