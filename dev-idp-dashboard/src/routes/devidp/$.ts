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

async function forward(splat: string, request: Request): Promise<Response> {
  const externalUrl = process.env["GRAM_DEVIDP_EXTERNAL_URL"];
  if (!externalUrl) {
    return Response.json(
      { error: "GRAM_DEVIDP_EXTERNAL_URL is not set on the dashboard server" },
      { status: 500 },
    );
  }

  const url = new URL(request.url);
  const target = `${externalUrl}/${splat}${url.search}`;

  const init: RequestInit = {
    method: request.method,
    headers: { Accept: request.headers.get("accept") ?? "application/json" },
  };
  if (request.method !== "GET" && request.method !== "HEAD") {
    init.body = await request.text();
    (init.headers as Record<string, string>)["Content-Type"] =
      request.headers.get("content-type") ?? "application/json";
  }

  const upstream = await fetch(target, init);
  const headers = new Headers();
  upstream.headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase())) headers.set(key, value);
  });
  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers,
  });
}

export const Route = createFileRoute("/devidp/$")({
  server: {
    handlers: {
      GET: ({ params, request }) => forward(params._splat ?? "", request),
      POST: ({ params, request }) => forward(params._splat ?? "", request),
    },
  },
});
