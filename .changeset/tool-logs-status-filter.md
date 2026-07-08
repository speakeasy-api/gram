---
"dashboard": patch
"server": patch
---

fix: apply the Tool Logs `http.response.status_code` filter at the trace level so status-less rows no longer leak 200/success traces into "Non-2xx responses", and add a first-class Error/Success/Blocked/Pending Status filter to the Tool Logs page.
