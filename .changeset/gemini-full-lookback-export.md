---
"server": patch
---

The first Gemini sync now imports the last 30 days in one Google Vault export that runs up to the time the sync starts, instead of one UTC day per export starting with the oldest. While an export runs, Speakeasy checks it every 5 minutes, so conversations appear soon after Vault finishes rather than over the next two and a half days. After that, each hourly sync exports everything since the previous export, so today's conversations no longer wait for the UTC day to end. An export too large to import is split into shorter time ranges. Changing the service account key or the organizational unit restarts the 30 days from that save, not from when Gemini was first connected. Connections already syncing day by day continue from where they are, up to the present, on their next sync.
