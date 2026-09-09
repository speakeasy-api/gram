import { createFileRoute } from "@tanstack/react-router";
import type { Backend, Mode } from "@/lib/devidp";
import { ENV_DOCS } from "@/lib/env-docs";

/**
 * Report the identity backend dev-idp is running. This is read straight from
 * GRAM_DEVIDP_BACKEND rather than inferred from URLs: both backends now mount
 * at the same prefixes, so there is nothing to sniff — and the old
 * prefix-matching heuristic silently reported the wrong backend once
 * WORKOS_API_URL stopped changing between them.
 */
function detectBackend(): Backend | null {
  switch (process.env["GRAM_DEVIDP_BACKEND"]) {
    case undefined:
    case "":
    case "local":
      return "local";
    case "workos":
      return "workos";
    default:
      return null;
  }
}

/** The currentUser slot that is authoritative for a given backend. */
function slotForBackend(backend: Backend): Mode {
  return backend === "workos" ? "workos" : "oauth2-1";
}

function buildEnvReadout() {
  return ENV_DOCS.map((doc) => {
    const raw = process.env[doc.name];
    const isSet = raw !== undefined && raw !== "" && raw !== "unset";
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
        const backend = detectBackend();
        if (!backend) {
          return Response.json(
            { error: 'GRAM_DEVIDP_BACKEND must be "local" or "workos"' },
            { status: 500 },
          );
        }
        const mode = slotForBackend(backend);
        const meta = { env: buildEnvReadout() };
        const dev = process.env["GRAM_DEVIDP_EXTERNAL_URL"];
        if (!dev) {
          return Response.json({ backend, mode, currentUser: null, meta });
        }
        let currentUser: unknown = null;
        try {
          const res = await fetch(`${dev}/rpc/devIdp.getCurrentUser`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ mode }),
          });
          if (res.ok) currentUser = await res.json();
        } catch {
          // Treat fetch failure as "no current user" — surface backend regardless.
        }
        return Response.json({ backend, mode, currentUser, meta });
      },
    },
  },
});
