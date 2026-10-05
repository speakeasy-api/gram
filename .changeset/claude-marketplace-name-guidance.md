---
"dashboard": patch
"server": patch
---

Claude Code install instructions now keep plugin auto-update working. Every managed-settings snippet says that the `extraKnownMarketplaces` key and the `@` suffix in `enabledPlugins` must be exactly the marketplace name shown, not the GitHub repository name or an older `<org>-gram` name, because Claude Code ignores `autoUpdate` under any other key. Each `/plugin marketplace add` command now tells users to turn on auto-update in `/plugin` → Marketplaces. The hooks setup dialog and the generated marketplace README used a `plugins.required` key that Claude Code does not read. They now use `enabledPlugins`, and the README snippet also sets `autoUpdate` and `FORCE_AUTOUPDATE_PLUGINS`.
