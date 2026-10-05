---
"server": minor
---

Send people to their organization's own host. When an organization's recorded default host is a different platform host from the one a login finishes on, the login callback now sends the browser on to sign in on that host, keeping the page they asked for. `auth.info` also returns `active_organization_dashboard_url` when the active organization lives on a different platform host from the request, so the dashboard can move an existing session there. Organizations without a recorded host, custom domains, private network ingress, and support or impersonation sessions are never moved.
