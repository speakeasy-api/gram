# Project — Remote OAuth Clients for Private Repos

Tickets and milestones tracking the work to land the design in `spike.md`. Milestones are described in `prompt.md` under "Structure → project.md"; this file is the live tracker.

## Cleanup

Tickets to remove dead or about-to-be-dead structure that the new design no longer needs. Each ticket should land as its own PR (separate from feature work).

- [ ] **Remove `AdditionalCacheKeys` from the cached-object interface.**
  - Today's `cache.CacheableObject[T]` interface (`server/internal/cache/cache.go:44`) requires every cached value to declare `AdditionalCacheKeys() []string` so that one logical record can be written under multiple Redis keys. The pattern was introduced for legacy `oauth.Token` (so that the same record was reachable by both access-token-hash and refresh-token-hash).
  - The new schema (§4.3) doesn't use multi-key fan-out anywhere. Each record is keyed exactly once. The method is now dead weight on every implementer.
  - **Action:** drop `AdditionalCacheKeys` from the interface; remove the per-implementer stub returns; simplify the cache write paths in `server/internal/cache/cache.go` (lines 68, 129, 152) that iterate the additional keys.
