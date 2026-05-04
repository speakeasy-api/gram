import { createFileRoute } from "@tanstack/react-router";
import type { Mode } from "@/lib/devidp";
import { ENV_DOCS } from "@/lib/env-docs";

/**
 * Detect which dev-idp mode the Gram server is currently configured against by
 * looking at the URLs Gram is told to call.
 *
 * Heuristic: if `SPEAKEASY_SERVER_ADDRESS` or `WORKOS_API_URL` starts with
 * `${GRAM_DEVIDP_EXTERNAL_URL}/<mode>`, that mode is active. If neither of
 * these env vars points back at the dev-idp, Gram is running against an
 * external upstream and we report `null`.
 *
 * `oauth2-1` is checked before `oauth2` so the longer prefix wins.
 */
function detectMode(): Mode | null {
  const dev = process.env["GRAM_DEVIDP_EXTERNAL_URL"];
  if (!dev) return null;
  const candidates = [
    process.env["SPEAKEASY_SERVER_ADDRESS"],
    process.env["WORKOS_API_URL"],
  ];
  const prefix = `${dev.replace(/\/$/, "")}/`;
  for (const url of candidates) {
    if (!url || !url.startsWith(prefix)) continue;
    const rest = url.slice(prefix.length);
    if (rest.startsWith("local-speakeasy")) return "local-speakeasy";
    if (rest.startsWith("oauth2-1")) return "oauth2-1";
    if (rest.startsWith("oauth2")) return "oauth2";
    if (rest.startsWith("workos")) return "workos";
  }
  return null;
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
