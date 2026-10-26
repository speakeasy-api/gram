---
"server": patch
"dashboard": patch
---

Hooks now fail open by default when they can't run, including when the hooks binary can't be installed on a machine (a missing tool or a blocked download). Before, a new machine whose download failed stopped agent tool calls before anything reached Speakeasy. Organizations that never changed the "Fail open when hooks can't run" setting now read as on, which also covers control-plane outages and timeouts. Organizations that turn it off keep blocking. A download that fails checksum verification never runs under either setting. The install-failure behavior is baked into published hook plugins and takes effect on the next plugin publish.
