---
"dashboard": patch
"server": patch
---

Logging out now returns `Clear-Site-Data: "cookies", "storage"`, so the browser drops the session cookie across the origin's registrable domain and empties localStorage, sessionStorage, IndexedDB and Cache Storage. Previously teardown relied entirely on an expiring `Set-Cookie` plus a best-effort localStorage sweep, both of which leave data behind when a cookie attribute drifts or a page navigates away mid-logout. The theme preference and project favorites still survive a logout: the dashboard reads them before the request goes out and writes them back once the response lands.
