---
"hooks": minor
---

Read the hooks debug log path from `GRAM_HOOKS_DEBUG_LOG` as well as the
`--debug-log=` flag. The flag stays the only channel that survives providers
which scrub the hook environment, so it remains authoritative where both name
a path; the variable supplements it for providers that pass the environment
through, letting support turn diagnostics on for an intermittent failure
without the customer hand-editing every hook command in a generated
`hooks.json` and undoing it afterwards. Every entrypoint that resolves a
config honours it — the per-event hook path, `login`, and `pi serve` — and the
`drain` process, which runs without those flags, reads it directly and reports
its replay counts there.
