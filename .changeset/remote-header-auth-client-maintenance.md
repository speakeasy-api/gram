---
"server": patch
---

A remote MCP source's credential headers now keep their typed upstream auth client as they are written: adding an Authorization header records the client, its copied headers and the server links that offer it; rotating the header's value updates the copy; and deleting the header, or the source, retires them. An MCP server created in front of a source that already carries headers picks up its client as it appears. Previously these records only arrived with the next organization-wide backfill run, so a source set up in between had none. Administrator-created clients, administrator detachments and credential choices are left untouched.
