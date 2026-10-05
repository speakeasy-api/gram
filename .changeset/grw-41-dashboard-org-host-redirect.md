---
"dashboard": minor
---

Move the dashboard to the active organization's own host. When `auth.info` reports that the active organization lives on a different platform host, the dashboard replaces the page with the same path, query, and hash on that host, where the user signs in once. Each tab moves to a given host at most once, so hosts can never bounce a tab back and forth, and support or impersonation sessions never move.
