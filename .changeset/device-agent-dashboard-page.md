---
"dashboard": minor
---

Add an org-level Device Agent page (`/<org>/device-agent`) under the Secure nav group: per-OS install instructions for the Speakeasy device agent, `managed.json` MDM configuration reference (schema, paths, example), and self-service `org_token` generation via the new `agent` API-key scope (mint/rotate from the page, with a ready-to-paste `managed.json` copied to the clipboard).

Also patches the CLI loopback callback (`CliCallback.tsx`) to append `&email=<userEmail>` to the redirect, which the device agent's one-shot OAuth-loopback enrollment requires.
