---
"server": patch
"admin": patch
---

The typed auth client backfill now also records static credential and pass-through clients from a source's credential headers, copies credential headers onto upstream OAuth clients, and records EMA clients from ready identity chaining bindings. It keeps copies and scopes in sync, retires what the configuration no longer yields, runs each phase independently under a per-organization lock, and reports per-kind counts and why anything is unresolved.
