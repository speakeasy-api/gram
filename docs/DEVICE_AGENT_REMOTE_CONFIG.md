# Device agent remote configuration

Gram stores one versioned, non-secret device-agent configuration document per
organization. Enrolled agents receive it in the existing
`agent.getPlugins` response, authenticated by their `agent_user` key (or the
legacy organization `agent` install key).

The dashboard reads and updates the same document through
`agent.getConfiguration` and `agent.updateConfiguration`. Both require
`org:admin`: the fleet configuration surface is organization administration, so
viewing it — not just changing it — is admin-only.

## Version 1 settings

The configuration document supports these keys:

- `platforms`: a map from tool name to `false`, `"user"`, or `"managed"`.
  `false` disables management, `user` writes in the user's home directory, and
  `managed` uses the elevated managed writer. Tool names must be 1 through 64
  characters.
- `update_channel`: optional non-empty string of at most 128 characters naming
  the release channel agents should follow. Platform-admin-only (see below).
- `auto_update`: optional non-empty string of at most 128 characters carrying
  the update mode understood by deployed agent versions.
- `pinned_target`: optional non-empty string of at most 128 characters pinning
  the fleet to a specific agent release.
- `blocked_versions`: optional array of at most 100 non-empty strings, each at
  most 128 characters, naming agent releases that must not be installed.
  Platform-admin-only (see below).
- `sync_interval_seconds`: optional whole number of seconds between
  reconciliations, 60 through 86,400.
- `ai_scan_interval_seconds`: optional whole number of seconds between Shadow
  AI scans, 60 through 86,400. Agents apply their own default (six hours) and
  clamp when the key is absent or out of range.

Every key is optional. The whole document must stay under 64 KiB. The envelope
carries `schema_version`; it is metadata, not a remotely editable setting.
Agents must ignore unknown document keys so additive settings remain forward
compatible. An update replaces the known settings above wholesale — omitting
one removes it — while Gram preserves stored keys the serving version does not
recognize, so an older server cannot delete settings understood only by newer
agents. Gram also rejects updates when the stored document carries a newer
`schema_version` than the serving version understands.

`update_channel` and `blocked_versions` are Speakeasy-internal release
controls: updates that include them are rejected unless the caller is a
Speakeasy platform administrator, and updates from org admins preserve any
stored values instead of removing them by omission. The dashboard only shows
these fields to platform administrators.

Gram rejects the device-local keys `email`, `org_token`, `org_slug`, `org_name`,
and `v`. Identity, credentials, and the local configuration schema always stay
on the device.

## Server-injected keys

Some keys in the document agents receive are not organization settings at all:
Gram adds them to the `config` map at serve time on `agent.getPlugins`, never
stores them, and rejects any update that tries to set them.

- `ai_scan`: the Shadow AI scan target catalog — the Speakeasy-curated list of
  AI tools the device agent probes for, with each target's on-device
  signatures (macOS bundle ids, PATH binaries, home-relative config
  directories, process names). The object carries `schema_version`,
  `list_version` (the catalog revision, which agents echo as
  `target_list_version` on every scan receipt; a receipt carries `0` when
  the agent scanned with the list embedded in its binary because it has not
  received one), `etag`, and `targets`.
  Platform administrators manage the catalog through the
  `platformAiScanTargets` service; org admins cannot change it, so an
  organization can never steer what the scanner probes for on employee
  devices. Agents validate every target before using it and fall back to
  their last cached list, then to the list embedded in their binary, when the
  key is absent or invalid.

The catalog is served whether or not the organization has saved settings. An
organization that has never saved settings still receives an envelope with
`is_configured: false`; agents keep their local policy for everything else and
read `ai_scan` on its own. The catalog etag is folded into the document etag,
so a catalog change moves the poll etag agents use to detect change.

## Resolution and offline behavior

Remote configuration is an IT-authoritative layer. After the first successful
fetch, its shareable settings override the same settings from local
`managed.json` and `local.json`; device-local identity and credentials are never
overridden.

The agent caches the last successfully fetched remote document. If Gram is
temporarily unavailable, it continues using that last-known document. If the
agent has never fetched a remote document (the response omits
`configuration`), it resolves only its local sources. This preserves the
existing fail-safe local behavior without silently discarding an already
applied organization policy during a transient outage.
