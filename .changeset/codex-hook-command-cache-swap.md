---
"server": patch
---

Stop Codex hooks failing with exit code 127. The hook command Codex caches for
a session pointed at one versioned plugin cache directory, so a background
plugin refresh that swapped that directory out left the session invoking a
bootstrap script that no longer existed — the shell reported "command not
found" and Codex surfaced it as `SessionStart`/`UserPromptSubmit` failures. The
bootstrap now persists itself and its deployment config together in Codex's
version-independent plugin data directory. Once ready, hook commands execute
that stable bundle and use the installed payload only to refresh it; the newest
cache sibling remains a first-run migration fallback. Unix and Windows both
honour the configured install-failure policy with an explanatory diagnostic
instead of an opaque missing-command failure.

Also fixes the trusted-hash computation for Codex hooks: it was serialising the
command with Go's HTML escaping, so any command containing `>`, `<`, or `&`
hashed differently than Codex computes it and the hook was silently dropped as
untrusted.
