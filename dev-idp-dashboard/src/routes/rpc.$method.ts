import { createFileRoute } from "@tanstack/react-router";

const HOP_BY_HOP = new Set([
  "connection",
  "keep-alive",
  "transfer-encoding",
  "upgrade",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
]);

export const Route = createFileRoute("/rpc/$method")({
  server: {
    handlers: {
      POST: async ({ params, request }) => {
        const externalUrl = process.env["GRAM_DEVIDP_EXTERNAL_URL"];
        if (!externalUrl) {
          return Response.json(
            {
              error:
                "GRAM_DEVIDP_EXTERNAL_URL is not set on the dashboard server",
            },
            { status: 500 },
          );
        }

        const target = `${externalUrl}/rpc/${params.method}`;
        const body = await request.text();

        const upstream = await fetch(target, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body,
        });

        const headers = new Headers();
        upstream.headers.forEach((value, key) => {
          if (!HOP_BY_HOP.has(key.toLowerCase())) {
            headers.set(key, value);
          }
        });

        return new Response(upstream.body, {
          status: upstream.status,
          statusText: upstream.statusText,
          headers,
        });
      },
    },
  },
});
