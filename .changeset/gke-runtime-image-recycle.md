---
"server": patch
---

Assistant runtimes on the GKE backend now roll onto the configured runtime image automatically. Previously a claimed sandbox kept its admission-time image forever unless the assistant went idle long enough for the inactivity janitor, so regularly used assistants never picked up deployed runner changes — and even a fresh claim could adopt a warm-pool pod pre-warmed on an older image. The deploy-time image recycle sweep now covers GKE runtimes (idle-gated claim re-adoption), turn admission recycles a stale claim lazily and drains stale warm-pool pods with bounded retries, and persisted runtime metadata records the pod's actual image instead of the configured one.
