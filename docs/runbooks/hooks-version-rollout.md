# Hooks Version Rollout

Use this runbook to roll a new observability hooks plugin version out to
customer organizations, or to change the hooks version one customer receives.

## How the rollout works

A hooks release ships with the server: merging a hooks change publishes a
`speakeasy-hooks` binary and opens a `chore(hooks): serve the <version> hooks
binary` PR that bumps `hooksGeneratorVersion` in
`server/internal/plugins/generate.go`. Deploying the server makes that the
**current hooks version**.

The current version does not reach customers on its own. Every organization
has a **pin**: the highest hooks version it is cleared to receive. The hourly
plugin rollout sweep republishes an organization's hooks plugin once its pin
reaches the current version. Lowering a pin holds back later versions; it never
downgrades a hooks plugin that is already published.

An organization's pin is:

1. **Canary**: canary organizations (`speakeasy-team`) always receive the
   current version. The list lives in code (`server/internal/hooksrollout`).
2. **Override**: an organization's own pin, when it has one. It wins over the
   default in both directions, so it can hold an organization back or roll a
   release out to it early.
3. **Default pin**: the platform-wide pin for every other organization.
4. **Legacy flag**: until a default pin is set, organizations without an
   override still follow the payload of the `hooks-rollout` PostHog flag. See
   [Cutover from PostHog](#cutover-from-posthog).

Pins are managed in the admin dashboard. Every change records who made it and
when, and the last 25 changes are listed on the **Hooks rollout** page.

## Roll out a new hooks version to every customer

1. Check that the hooks release is deployed. Open the admin dashboard and go to
   **Platform Management** → **Hooks rollout** (`/hooks-rollout`). **Current
   hooks version** is read from the running server, so it shows the new version
   only once the release PR is merged and deployed.
2. Optional: check the canary first. Canary organizations receive the current
   version on the next sweep without a pin change. Give it the soak time the
   release needs before you continue.
3. Review **Organization overrides**. An organization with an override does not
   follow the default pin. Clear any override that should now follow the
   default, from that organization's **Features** tab.
4. Under **Default pin**, enter the current hooks version and click **Set
   default pin**. Confirm the dialog.
5. Wait for the next hourly plugin rollout sweep. To check an organization,
   open it and go to the **Features** tab: **Hooks version rollout** reads
   **Cleared for version `<current>`**. Its published hooks plugin version is
   `0.<current>.<publish timestamp>` in the marketplace repository.

## Change the hooks version for one customer

1. Open the organization in the admin dashboard and go to the **Features** tab.
2. Under **Hooks version rollout**, enter the version and click **Set
   override** (or **Update override**):
   - To roll a release out early, enter the current hooks version.
   - To hold the organization back, enter the last version it should receive.
3. To make the organization follow the default pin again, click **Clear
   override**.

The change reaches the organization on the next hourly rollout sweep. Canary
organizations have no override control because they ignore pins.

## Hold back a release

Leave the default pin where it is. Organizations without an override stay at
or below it, whatever the current hooks version is. To stop a release that has
already started rolling out, set the default pin back to the previous version:
organizations that have not been republished yet stay on their current hooks
plugin, and organizations already on the new version keep it until the next
version they are cleared for.

## Cutover from PostHog

Before this runbook, the rollout was controlled by the `{"version": N}` payload
of the `hooks-rollout` PostHog flag. The server still reads that payload, but
only for organizations without an override while no default pin is set. To move
the rollout off PostHog without changing what any customer receives:

1. In PostHog, open the `hooks-rollout` flag and note the payload version of
   the release condition that matches all organizations, and of every condition
   that targets specific organizations.
2. For each organization a condition targets with a different version, set that
   version as an override on the organization's **Features** tab.
3. Set the default pin to the version of the condition that matches all
   organizations.
4. Check that organizations without an override now show **Follows the default
   pin** on their **Features** tab. From this point the PostHog flag is no
   longer read and can be removed together with `FlagHooksRollout` in
   `server/internal/feature/flags.go`.

## Troubleshooting

- **Eligibility reads Unknown.** No pin applies to the organization, so the
  legacy PostHog flag decides and the admin server cannot read it. Set the
  default pin or an override.
- **The organization is cleared but still has the old hooks plugin.** The
  sweep runs hourly. If it has run since the change, check the worker logs for
  `plugin generator rollout complete`: `failed`, `conflicted` and `rejected`
  count projects the sweep could not republish. A project with observability
  disabled publishes no hooks plugin at all.
- **The pin cannot be set above a version.** A pin above the current hooks
  version is refused, because it would clear future releases before anyone
  decided to roll them out.

The staff Admin MCP exposes the same state read-only through the
`get_hooks_rollout` and `get_organization_hooks_rollout` tools. Pin changes are
made from the dashboard only.
