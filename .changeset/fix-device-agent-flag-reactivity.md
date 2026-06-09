---
"dashboard": patch
---

Fix feature-flag gated UI (e.g. the Device Agent nav link) staying hidden even when the flag is on. PostHog loads feature flags asynchronously, but `useTelemetry()` consumers never re-rendered once flags resolved, so `isFeatureEnabled(...)` reads were stuck on their pre-load value. `useTelemetry` now subscribes to PostHog's `onFeatureFlags` event and re-renders consumers when flags resolve or change, making every `isFeatureEnabled` call site reactive.
