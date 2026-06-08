---
"dashboard": patch
---

Fix the Device Agent nav link staying hidden even when the `gram-device-agent` flag is on. PostHog loads feature flags asynchronously, but the nav item read the flag with a non-reactive `isFeatureEnabled` call that never re-rendered once the flag resolved. A new reactive `useFeatureFlag` hook subscribes to PostHog's `onFeatureFlags` event so opt-in flag gates flip on reliably.
