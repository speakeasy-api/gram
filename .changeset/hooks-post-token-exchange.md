---
"server": minor
"dashboard": minor
---

Hook browser login now delivers the minted API key to the local listener as a form POST instead of appending it to the callback URL, keeping the key out of browser history and request logs, and the sign-in tab closes itself once authentication completes. Older dashboards that still redirect with query parameters keep working.
