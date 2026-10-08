import { createFileRoute } from "@tanstack/react-router";

/**
 * Server-side session proxy. This route keeps the SPEAKEASY_AI_API_KEY secret by
 * making the upstream call to Gram's session API from the server, never
 * exposing the key to the browser. The client calls this endpoint via
 * the `session` field in the ElementsConfig (see chat.tsx).
 */
export const Route = createFileRoute("/api/chat/session")({
  server: {
    handlers: {
      POST: async ({ request }: { request: Request }) => {
        const base =
          process.env.SPEAKEASY_AI_API_URL ?? "https://app.getgram.ai";
        const body = await request.json();

        // Proxy the session creation request to Gram's API, attaching
        // server-only credentials (SPEAKEASY_AI_API_KEY) that the client can't see.
        const upstream = await fetch(base + "/rpc/chatSessions.create", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Speakeasy-AI-Project":
              process.env.VITE_SPEAKEASY_AI_PROJECT_SLUG ?? "",
            "X-Speakeasy-AI-Key": process.env.SPEAKEASY_AI_API_KEY ?? "",
          },
          body: JSON.stringify({
            embed_origin: body.embed_origin,
            user_identifier: body.user_identifier,
            expires_after: body.expires_after,
          }),
        });

        const text = await upstream.text();
        return new Response(text, {
          status: upstream.status,
          headers: { "Content-Type": "application/json" },
        });
      },
    },
  },
});
