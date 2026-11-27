---
"dashboard": patch
---

Setup instructions the dashboard generates now name the Speakeasy host they were generated on when a client's default host would not reach it, for example on another region's deployment. This covers the device agent `managed.json`, macOS configuration profile, Intune package and cloud environment script (`_control_plane_url`), the `speakeasy-hooks install` commands (`--server-url`), and the CLI instructions (a `speakeasy auth --api-url` step). On such a host, personal device agent sign-in now warns that it reaches only the agent's default host and points to `managed.json`, and the CI push instructions mention `SPEAKEASY_AI_API_URL`. Instructions generated on the US hosts are unchanged.
