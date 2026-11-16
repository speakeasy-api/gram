---
"server": patch
---

Tool and annotation limits now apply to using an MCP server only. A view or manage rule, or a block on either, that names tools or annotations is refused when saved. Rules saved earlier with such a limit keep the effect they already had: a limited view or manage rule works as an unlimited one, and a limited block on view or manage does nothing.
