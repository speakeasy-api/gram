---
"server": minor
"dashboard": patch
---

Access rules that limit an MCP server to tools with certain annotations (for example "read-only") no longer reach tools without a recognized annotation hint. Previously such a rule also allowed listing and calling every tool that had no annotation hint set to true — a tool with no annotations, only a title, or every hint false, and for remote and tunneled MCP servers any tool with no stored tool metadata. This applies to hosted toolsets, remote MCP servers and tunneled MCP servers. Rules for a whole server or project, and rules naming only a tool, are unchanged; a rule naming a tool and also an annotation now reaches that tool only when it carries the annotation.

Tools a hosted toolset passes through from an external MCP server are always treated as unannotated for access rules, even when the external server annotates them, because those annotations come from the external server at request time. Annotation rules neither grant them nor exclude them; grant or exclude them by name or by server.

Every surface now applies the same check. In dynamic mode (`Gram-Mode: dynamic`), `search_tools` and `describe_tools` only find the tools the caller may call, and a caller who may call none sees no tools. Selecting every annotation in the role editor is not the same as granting the whole server; the editor now says so.

To restore access to an affected tool, record its correct annotations (on the hosted tool definition, or as stored tool metadata from a remote or tunneled server's Inspect tab) or grant it by name. The Platform MCP `get_mcp_access` tool reports which known tools a role reaches, but it only knows tools that are defined or stored; a remote or tunneled server's other tools are found by listing it from its Inspect tab, which may show each user a different set.
