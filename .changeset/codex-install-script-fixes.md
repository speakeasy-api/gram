---
"server": patch
---

The Codex observability plugin install script now works on machines where the `codex` CLI is not on PATH: it probes well-known install locations, including the Codex desktop app bundle, before falling back to manual instructions. It also writes feature flags inside the `[features]` table instead of as root-level dotted keys, fixing a "duplicate key" config error on machines whose `config.toml` already has a `[features]` table, and cleans up dotted keys left behind by earlier versions of the script.
