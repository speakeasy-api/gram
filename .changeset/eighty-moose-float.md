---
"server": patch
---

Moved control server initialization after all routes and middleware are attached, and added a /healthz endpoint to the main API mux so the control server can verify the API is actually serving traffic before reporting healthy.
