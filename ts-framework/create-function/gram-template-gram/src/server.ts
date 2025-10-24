import { serve } from "@hono/node-server";
import { Hono } from "hono";
import { gram } from "./gram.ts";

const app = new Hono();

app.post("/tool-call", async (c) => {
  const req = await c.req.json();
  return await gram.handleToolCall(req, {
    signal: AbortSignal.timeout(1 * 60 * 1000),
  });
});

app.get("/manifest", async () => {
  const manifest = gram.manifest();
  return new Response(JSON.stringify(manifest, null, 2), {
    headers: { "Content-Type": "application/json" },
  });
});

export function startServer(port: number = 3000) {
  const server = serve({ fetch: app.fetch, port }, (info) => {
    console.log(`Listening on http://localhost:${info.port}`);
  });

  const onClose = () => {
    console.log("Shutting down server...");
    server.close((err) => {
      if (err) {
        console.error(err);
        process.exit(1);
      }
      process.exit(0);
    });
  };
  process.on("SIGINT", onClose);
  process.on("SIGTERM", onClose);
}

if (import.meta.main) {
  startServer();
}

export default app;
