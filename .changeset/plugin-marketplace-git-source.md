---
"server": patch
"dashboard": patch
---

Fix Claude Code plugins not loading after restart. The `git-subdir` source
type used by the marketplace proxy does not persist the plugin cache path
across Claude Code sessions, causing "not cached at (not recorded)" errors
on every relaunch. The marketplace URL returned by `getPublishStatus` now
points directly at the git proxy (`/marketplace/p/{token}.git`) and the
install instructions emit `"source": "git"` in the `extraKnownMarketplaces`
snippet, which Claude Code caches reliably between sessions. The
URL-based manifest endpoint and its rewrite logic have been removed.
