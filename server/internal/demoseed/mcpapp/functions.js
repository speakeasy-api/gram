// The seeded Gram Function backing the playground's MCP App example. It is
// zipped alongside a generated manifest.json by the local seed and uploaded as
// the function asset; the runner executes this module directly.
//
// HTML is injected ahead of this file's contents at seed time (see
// mcpapp.go) so app.html stays a real, editable HTML file rather than a
// string literal.

const TOOL_NAME = "show_dashboard";
const RESOURCE_URI = "ui://playground-mcp-app/dashboard";
const FUNCTION_SLUG = "playground-mcp-app";

export default {
  async handleToolCall({ name, input }) {
    if (name !== TOOL_NAME) {
      return new Response(JSON.stringify({ error: "Unknown tool", name }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }

    const query =
      typeof input?.query === "string" && input.query.trim().length > 0
        ? input.query.trim()
        : "Gram MCP Apps";

    const payload = {
      slug: FUNCTION_SLUG,
      query,
      generatedAt: new Date().toISOString(),
      cards: [
        "UI metadata comes from the tool + resource definitions",
        "The playground host fetches the UI resource with resources/read",
        "The iframe receives tool-input and tool-result notifications",
      ],
    };

    return new Response(JSON.stringify(payload), {
      headers: { "Content-Type": "application/json" },
    });
  },

  async handleResources({ uri }) {
    if (uri !== RESOURCE_URI) {
      return new Response("Unknown resource", {
        status: 404,
        headers: { "Content-Type": "text/plain" },
      });
    }

    return new Response(HTML, {
      headers: { "Content-Type": "text/html;profile=mcp-app" },
    });
  },
};
