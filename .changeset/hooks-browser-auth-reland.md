---
"server": minor
"dashboard": patch
---

Re-introduce the unified `/rpc/hooks.ingest` endpoint with working self-serve authentication for hook plugins. On session start the plugin opens the Gram dashboard in a browser, receives a hooks-scoped API key on a localhost callback, and caches it per device — no python or manual key setup required. Machines that have never authenticated are not blocked: sessions proceed with a warning, Claude is prompted to offer connecting via the bundled login helper, and enforcement only becomes strict after the first successful sign-in.
