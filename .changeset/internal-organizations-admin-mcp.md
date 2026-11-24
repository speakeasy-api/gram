---
"server": patch
---

The staff Admin MCP now says whether an organization is marked internal, in `find_organizations` results and in `get_organization_summary`. `find_organizations` also takes an `internal_status` filter, so an agent can list every internal organization in one call and leave them out of customer analysis.
