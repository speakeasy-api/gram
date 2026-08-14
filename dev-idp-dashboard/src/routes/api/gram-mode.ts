import { createFileRoute } from "@tanstack/react-router";
import type { Mode } from "@/lib/devidp";
import { ENV_DOCS } from "@/lib/env-docs";

/**
 * Detect which dev-idp mode the Gram server is currently configured against by
 * looking at the URLs Gram is told to call.
 *
 * Heuristic: if `WORKOS_API_URL` or `GRAM_IDP_BASE_URL` addresses this dev-idp,
 * its first path segment names the active mode. If neither points back at the
 * dev-idp, Gram is running against an external upstream and we report `null`.
 *
 * Matching is on PORT, not on the whole origin. `GRAM_IDP_BASE_URL` is
 * browser-facing and `GRAM_DEVIDP_EXTERNAL_URL` is dialed by the server, so
 * `zero:remap-hostname` legitimately puts them on different hosts; comparing
 * full-origin prefixes made this silently report the wrong mode whenever remote
 * access was on. The port is assigned once per worktree and shared by both.
 *
 * `oauth2-1` is checked before `oauth2` so the longer prefix wins.
 */
function detectMode(): Mode | null {
  const dev = parseURL(process.env["GRAM_DEVIDP_EXTERNAL_URL"]);
  if (!dev) return null;

  const candidates = [
    process.env["WORKOS_API_URL"],
    process.env["GRAM_IDP_BASE_URL"],
  ];

  for (const candidate of candidates) {
    const url = parseURL(candidate);
    if (!url || url.port !== dev.port) continue;
    const rest = url.pathname.replace(/^\//, "");
    if (rest.startsWith("mock-workos")) return "mock-workos";
    if (rest.startsWith("oauth2-1")) return "oauth2-1";
    if (rest.startsWith("oauth2")) return "oauth2";
    if (rest.startsWith("workos")) return "workos";
  }
  return null;
}

function parseURL(value: string | undefined): URL | null {
  if (!value) return null;
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

function buildEnvReadout() {
  return ENV_DOCS.map((doc) => {
    const raw = process.env[doc.name];
    const isSet = raw !== undefined && raw !== "";
    return {
      name: doc.name,
      description: doc.description,
      sensitive: Boolean(doc.sensitive),
      is_set: isSet,
      // Mask sensitive values; only expose the actual string for non-sensitive
      // vars when present.
      value: doc.sensitive ? null : isSet ? (raw as string) : null,
    };
  });
}

export const Route = createFileRoute("/api/gram-mode")({
  server: {
    handlers: {
      GET: async () => {
        const mode = detectMode();
        const meta = { env: buildEnvReadout() };
        if (!mode) {
          return Response.json({ mode: null, currentUser: null, meta });
        }
        const dev = process.env["GRAM_DEVIDP_EXTERNAL_URL"]!;
        let currentUser: unknown = null;
        try {
          const res = await fetch(`${dev}/rpc/devIdp.getCurrentUser`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ mode }),
          });
          if (res.ok) currentUser = await res.json();
        } catch {
          // Treat fetch failure as "no current user" — surface mode regardless.
        }
        return Response.json({ mode, currentUser, meta });
      },
    },
  },
});
