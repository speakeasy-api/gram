---
"server": minor
"dashboard": minor
---

Plugin assignments: organizations using the Speakeasy device agent can now choose which principals receive each plugin. From a plugin's detail page, admins assign an org-wide default (everyone), specific roles, individual members, or email addresses, and the device agent (`agent.getPlugins`) delivers each plugin only to its resolved recipients (email, user, and RBAC role membership). New plugins — including the auto-provisioned Default plugin — default to everyone, so nothing stops being delivered; admins can narrow the audience afterward. The assignments section is shown only for device-agent organizations; marketplace installs (Claude, Cursor, Codex) continue to receive every published plugin regardless of assignment.
