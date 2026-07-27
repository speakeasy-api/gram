# Rich Google Docs with the Workspace MCP

Gram installs `io.github.taylorwilsdon/workspace-mcp` from the Pulse registry's
latest version; it does not pin a connector release. The catalog detail page
shows the version and live tool list returned by Pulse. The upstream tier
manifest includes `import_to_google_doc` in Core alongside the plain-text
`create_doc` tool.

For a new document with headings, formatted text, lists, or tables, call
`import_to_google_doc` with the full document body as HTML and
`source_format: "html"`. Catalog installs expose this tool to assistants as
`create_rich_doc`. Continue to use `create_doc` when the initial content is
genuinely plain text.

The rich import is available in Core. In-place editing has separate tier
requirements:

- Extended: `insert_doc_elements`, `update_paragraph_style`, and related
  paragraph/table operations.
- Complete: `batch_update_doc`, `create_table_with_data`, and advanced table
  inspection or editing.

## Repeatable assistant check

1. Add Google Workspace from the Gram catalog and complete Google
   authentication.
2. Create an assistant with that MCP server attached.
3. Send:

   > Create me a Google Doc with example tables, a basic RBAC permissions
   > structure, dummy text, and sections with headings.

4. Inspect the chat transcript. The first creation call must be
   `create_rich_doc` and its forwarded upstream call must be
   `import_to_google_doc` with `source_format: "html"`. It must not use
   `create_doc`.
5. Ask the assistant to read the document back with `get_doc_content`.
6. Verify the returned native Google Doc contains at least two heading
   sections, formatted text, and a table with role and permission columns.

For local testing, the `assistants-dev` MCP server's `run_turn` and `load_chat`
tools expose the tool calls and read-back messages needed for steps 3–6.
