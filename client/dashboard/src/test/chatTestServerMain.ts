// Runs the chat test server as its own Node process and prints its base URL.
// DOM-environment tests use this through chatTestServerProcess.ts: inside a
// happy-dom test the server would build its responses with happy-dom's
// Response class, which drops the MCP server's text/event-stream content type.
import { startChatTestServer } from "./chatTestServer.ts";

const server = await startChatTestServer();
process.stdout.write(`${JSON.stringify({ baseUrl: server.baseUrl })}\n`);

const shutdown = () => {
  void server.close().finally(() => process.exit(0));
};
process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);
// Exit with the test worker even if it never sends a signal.
process.stdin.on("end", shutdown);
process.stdin.resume();
