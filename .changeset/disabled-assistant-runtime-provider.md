---
"server": minor
"dashboard": patch
---

Deployments can turn assistants off with `GRAM_ASSISTANT_RUNTIME_PROVIDER=disabled`. The server and worker then start without a GKE cluster, Kit or snapshot settings, and build no Kit configuration. Assistant endpoints that would start or drive a runtime answer `501` with "assistants are not available on this deployment", assistant triggers cannot be created, updated or resumed, trigger dispatch to assistants is dropped, the Speakeasy Slack app replies that assistants are unavailable, and the runtime reaper and janitor schedules are not registered. The organization product features report the new `assistants_available` field, and the dashboard hides the Project Assistant, Assistants, Chat and their Command K entries when it is false. The `gke` and `local` providers behave as before.
