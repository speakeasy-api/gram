---
"server": minor
"dashboard": minor
---

A grant made directly to a person for a specific resource now outranks a block they inherit from a role or from everyone. Administrators can block an MCP server for a role, including a directory-synced one, and still give individual members of that role access to it by name without changing role membership. A person's own blocks still apply, grants covering every resource do not outrank blocks, and agent grants never outrank blocks. The rule applies to every blockable permission (the organization, projects, MCP servers, environments, skills, plugins, and workloads). `access.listGrants` now reports each scope's `direct_selectors`, and the MCP server access page shows who keeps access through their own rules before a role or everyone is removed from a server.
