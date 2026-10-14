---
"dashboard": minor
"server": minor
---

With directory sync on, a role's page now lists the directory groups and attribute values that give out the role, in place of the read-only member list, with links to IDP and SSO, where mappings are changed. A role nothing is mapped to says so and offers to map a group.

The Platform MCP's role listing now names each role's directory groups and attribute values too, with how many directory users each matches; small counts are withheld, and a mapping whose group was deleted is flagged as removed.

An MCP server whose identity is set to Manual now says "Manual" on its details pill, matching the setting, instead of "None".
