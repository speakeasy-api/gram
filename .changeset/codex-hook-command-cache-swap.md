---
"server": patch
---

Stop Codex hooks failing with exit code 127. The hook command Codex caches for
a session pointed at one versioned plugin cache directory, so a background
plugin refresh that swapped that directory out left the session invoking a
bootstrap script that no longer existed — the shell reported "command not
found" and Codex surfaced it as `SessionStart`/`UserPromptSubmit` failures. The
command now falls back to the newest sibling version in the cache, and when no
bootstrap script can be found at all it explains itself on stderr and honours
the install-failure policy instead of exiting 127.

Also fixes the trusted-hash computation for Codex hooks: it was serialising the
command with Go's HTML escaping, so any command containing `>`, `<`, or `&`
hashed differently than Codex computes it and the hook was silently dropped as
untrusted.
