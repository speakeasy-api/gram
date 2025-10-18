/**
 * @param {{
 *   uri: "file:///config.json",
 *   input: never
 * } | {
 *   uri: "file:///data/users.csv",
 *   input: { limit?: number }
 * } | {
 *  uri: "https://api.example.com/status",
 *  input: never
 * } | {
 *  uri: "file:///templates/email.html",
 *  input: { name: string }
 * } | {
 *  uri: "error://fail",
 *  input: never
 * } | {
 *  uri: "null://resource",
 *  input: never
 * }} call
 * @returns
 */
export async function handleResources({ uri, input }) {
  switch (uri) {
    case "file:///config.json":
      return new Response(
        JSON.stringify({ version: "1.0.0", environment: "production" }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    case "file:///data/users.csv":
      const limit = input?.limit || 10;
      const csvData = Array.from({ length: limit }, (_, i) =>
        `user${i + 1},user${i + 1}@example.com`
      ).join("\n");
      return new Response(`name,email\n${csvData}`, {
        status: 200,
        headers: { "Content-Type": "text/csv" },
      });
    case "https://api.example.com/status":
      return new Response(
        JSON.stringify({ status: "ok", uptime: 12345 }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    case "file:///templates/email.html":
      return new Response(
        `<html><body>Hello, ${input.name}!</body></html>`,
        { status: 200, headers: { "Content-Type": "text/html" } },
      );
    case "error://fail":
      throw new Error("Resource access failed");
    case "null://resource":
      return null;
    default:
      return new Response(
        JSON.stringify({ error: "Resource not found" }),
        { status: 404, headers: { "Content-Type": "application/json" } },
      );
  }
}
