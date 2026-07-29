# Device agent remote configuration

Gram stores one versioned, non-secret device-agent configuration document per
organization. Enrolled agents receive it in the existing
`agent.getPlugins` response, authenticated by their `agent_user` key (or the
legacy organization `agent` install key).

The dashboard reads and updates the same document through
`agent.getConfiguration` and `agent.updateConfiguration`. Reads require
`org:read`; updates require `org:admin`.

## Version 1 settings

The configuration document supports these keys:

- `platforms`: a map from tool name to `false`, `"user"`, or `"managed"`.
  `false` disables management, `user` writes in the user's home directory, and
  `managed` uses the elevated managed writer.
- `update_channel`
- `auto_update`
- `pinned_target`
- `blocked_versions`
- `sync_interval_seconds` (60 through 86,400)

The envelope carries `schema_version`; it is metadata, not a remotely editable
setting. Agents must ignore unknown document keys so additive settings remain
forward compatible. Gram preserves unknown keys when the dashboard edits known
settings.

Gram rejects the device-local keys `email`, `org_token`, `org_slug`, `org_name`,
and `v`. Identity, credentials, and the local configuration schema always stay
on the device.

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
