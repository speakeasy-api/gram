// Package mcpskills resolves the managed skills a hosted MCP toolset endpoint
// serves to one caller under the Skills extension (SEP-2640), and records the
// activations those callers make by reading a served skill.
//
// A caller receives the skills of every plugin that contains the endpoint's
// backend, belongs to the endpoint's project, and is assigned to one of the
// caller's delivery principals. Each distribution resolves to its pinned
// version or else the skill's latest valid version, and when several plugins
// carry the same skill the resolver keeps exactly one version: a pin beats a
// distribution that tracks the latest version, and between two different pins
// the newer version wins.
//
// Every served skill is a single SKILL.md file addressed as
// skill://<name>/SKILL.md; the package produces the SEP-2640 entry shape for
// it, and the MCP surfaces decide when the extension is advertised and how
// its methods are dispatched.
package mcpskills
