---
"server": minor
"dashboard": patch
---

Add a shared Connections view for project and Organization Gateways, with organization-wide upstream visibility, identity search, server/provider association filters, creation dates, and authorization ordering. Apply filters before cursor pagination and support loading subsequent pages and retrying errors on gateway and provider-client session lists. Expose upstream authorization deadlines separately from access-token and refresh-token expiry.
