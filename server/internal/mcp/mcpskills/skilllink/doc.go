// Package skilllink holds the write side of mcp_skill_links: the MCP surfaces
// a skill can be linked to and the functions that create, update and remove
// links. It depends on nothing but the database layer, so the skills
// management service can write links without importing the resolver in
// package mcpskills, which itself depends on the skills package.
package skilllink
