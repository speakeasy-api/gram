// Package mcpskills resolves the managed skills an MCP surface serves under
// the Skills extension (SEP-2640), and records the activations callers make by
// reading a served skill.
//
// Skills reach a surface through mcp_skill_links rows that name it as a
// [Target]: a stored MCP server, a stored meta MCP gateway, or a row-less
// surface such as the organization gateway. A gateway also serves the skills
// linked to the member servers it passes in. Any live member of the
// surface's organization who reaches the surface receives its skills. Each
// link resolves to its pinned version or else the skill's latest valid
// version, and when several links carry the same skill the resolver keeps
// exactly one version: a pin beats a link that tracks the latest version, and
// between two different pins the newer version wins. Skills from different
// projects that share a name are separate skills and are all served.
//
// Every served skill is a single SKILL.md file addressed as
// skill://<project-slug>/<name>/SKILL.md; the project slug keeps same-named
// skills apart, and the final path segment is the frontmatter name SEP-2640
// requires. The package produces the SEP-2640 entry shape for
// it, and the MCP surfaces decide when the extension is advertised and how
// its methods are dispatched.
package mcpskills
