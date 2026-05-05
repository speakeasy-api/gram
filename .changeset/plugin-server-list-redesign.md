---
"dashboard": patch
---

Redesign the MCP servers list on the plugin detail page so each entry
matches the card pattern from the MCP list page: the Network icon in
the dot-pattern sidebar, name plus tool-count badge in the header, and
the Public / Private / Disabled status indicator on the footer left.
The footer right has a trash icon button that removes the server from
the plugin, and servers whose toolset has been deleted are flagged
inline. Also extracts the shared status indicator from MCPCard,
MCPTableRow, and the new card into a reusable
`MCPStatusIndicator` component.
