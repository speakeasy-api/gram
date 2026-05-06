---
"server": minor
"dashboard": minor
"sdk": minor
---

Add a marketplace proxy that fronts published plugin GitHub repos so users can install Gram-published plugins in Claude Code via `/plugin marketplace add` without making the upstream repo public.

- New routes on the existing server: `GET /m/{token}/marketplace.json` (URL-based Claude Code marketplace) and `/p/{token}.git/...` (git Smart HTTP proxy for plugin source clones). Both stream from GitHub via the same App installation token used for publishing — no local mirror state.
- `plugin_github_connections` gains a nullable `marketplace_token` column with a partial unique index. Tokens are auto-minted on first publish and preserved across subsequent publishes.
- `plugins.getPublishStatus` returns a new `marketplace_url` field that surfaces the `/plugin marketplace add` URL once a token has been minted. The dashboard's plugin publishing card shows it with a copy-the-install-command affordance.
