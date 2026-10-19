import { shellQuote } from "@/lib/shell";

// Ways to add one remote (Streamable HTTP) MCP server to an AI client, by URL.
// Each follows the client's own docs; check them again before changing a shape.

/** `claude mcp add`, for the signed-in user across every project. */
export function claudeCodeAddCommand(name: string, url: string): string {
  return `claude mcp add --transport http --scope user ${shellQuote(name)} ${shellQuote(url)}`;
}

/** `codex mcp add` for a Streamable HTTP server. */
export function codexAddCommand(name: string, url: string): string {
  return `codex mcp add ${shellQuote(name)} --url ${shellQuote(url)}`;
}

/** Codex does not start OAuth on add; this signs in to the server. */
export function codexLoginCommand(name: string): string {
  return `codex mcp login ${shellQuote(name)}`;
}

/**
 * Cursor's MCP install deeplink: opening it asks Cursor to add the server.
 * `config` is the base64 of the server's mcp.json entry.
 */
export function cursorInstallLink(name: string, url: string): string {
  const config = btoa(JSON.stringify({ url }));
  return `cursor://anysphere.cursor-deeplink/mcp/install?name=${encodeURIComponent(name)}&config=${encodeURIComponent(config)}`;
}

/** The `mcp` block to merge into opencode.json. */
export function openCodeConfig(name: string, url: string): string {
  return JSON.stringify(
    { mcp: { [name]: { type: "remote", url, enabled: true } } },
    null,
    2,
  );
}
