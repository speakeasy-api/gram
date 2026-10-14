---
"dashboard": minor
"server": minor
---

With directory sync on, a role's page now lists the directory groups and attribute values that give out the role, in place of the read-only member list, and links each one to its mapping on IDP and SSO. A role nothing is mapped to says so and offers to map a group, and people who hold the role through the identity provider's own rules, without a mapping here, are counted under the list.

The Platform MCP's role listing now names each role's directory groups and attribute values too, with how many directory users each matches; small counts are withheld.
