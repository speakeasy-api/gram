---
"server": patch
---

Downloading an OpenAPI document, function archive, or chat attachment, and signing a chat attachment URL, now authorize the project named in the request instead of only checking that it belongs to the caller's organization. A project-scoped API key, a chat session token, or a `Gram-Project` header now limits the request to that project, and dashboard sessions and user-created chat session tokens need `project:read` on it. Requests for another project are rejected with 403.
