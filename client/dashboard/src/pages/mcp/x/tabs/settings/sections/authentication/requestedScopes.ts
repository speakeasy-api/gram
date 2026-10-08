import { issuerDisplayName } from "@/lib/remote-identity";
import type { RemoteMcpServerClientScopes } from "@gram/client/models/components/remotemcpserverclientscopes.js";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";

type ScopeSource = RemoteMcpServerClientScopes["scopeSource"];

// Identity scopes every login asks for; they say nothing about the server's access.
const FEATURE_SCOPES = new Set([
  "openid",
  "email",
  "profile",
  "offline_access",
]);

/** Each source as a standalone line, and mid-sentence after "Sign-ins request <scopes>, ". */
const SOURCES: Record<ScopeSource, { line: string; phrase: string } | null> = {
  resource_pin: {
    line: "Pinned for this MCP server's URL.",
    phrase: "pinned for this MCP server's URL",
  },
  client_scope: {
    line: "Set on this connection.",
    phrase: "set on this connection",
  },
  challenge_scope: {
    line: "From the MCP server's last sign-in challenge.",
    phrase: "from the MCP server's last sign-in challenge",
  },
  live_resource: {
    line: "Advertised by the MCP server.",
    phrase: "advertised by the MCP server",
  },
  cached_resource: {
    line: "Advertised by the MCP server.",
    phrase: "advertised by the MCP server",
  },
  issuer_override: {
    line: "Set by the identity provider's override.",
    phrase: "set by the identity provider's override",
  },
  issuer_catalogue: {
    line: "Every scope the identity provider advertises.",
    phrase: "",
  },
  issuer_omitted: null,
  none: null,
};

// Sources a live read of the MCP server can outrank at the next sign-in.
const PROBE_CAN_CHANGE = new Set<ScopeSource>([
  "cached_resource",
  "issuer_override",
  "issuer_catalogue",
]);

const MAY_CHANGE = "This may change once the MCP server is next contacted.";

export function shownScopes(client: RemoteMcpServerClientScopes): string[] {
  return client.requestedScopes.filter((scope) => !FEATURE_SCOPES.has(scope));
}

export function requestsNothing(client: RemoteMcpServerClientScopes): boolean {
  return (
    client.scopeSource === "issuer_omitted" ||
    client.scopeSource === "none" ||
    client.requestedScopes.length === 0
  );
}

export function issuerLabel(client: RemoteMcpServerClientScopes): string {
  return (
    issuerDisplayName({
      name: client.issuerName,
      issuer: client.issuerUrl ?? "",
    }) || "the identity provider"
  );
}

function mayChange(
  client: RemoteMcpServerClientScopes,
  scopes: RemoteMcpServerScopes,
): boolean {
  return (
    PROBE_CAN_CHANGE.has(client.scopeSource) &&
    client.pinWouldDecide &&
    scopes.discoveryEnabled &&
    !scopes.advertisedScopesKnown
  );
}

/** The source line under the read-only summary. */
export function sourceLine(
  client: RemoteMcpServerClientScopes,
  scopes: RemoteMcpServerScopes,
): string | null {
  const parts = [SOURCES[client.scopeSource]?.line];
  if (mayChange(client, scopes)) parts.push(MAY_CHANGE);
  const line = parts.filter(Boolean).join(" ");
  return line || null;
}

/** One sentence naming what the client's sign-ins request, for the pin's status. */
export function requestedSentence(
  client: RemoteMcpServerClientScopes,
  scopes: RemoteMcpServerScopes,
): string | null {
  let sentence: string;
  if (requestsNothing(client)) {
    sentence = `Sign-ins request no scopes; ${issuerLabel(client)} applies its defaults.`;
  } else if (client.scopeSource === "issuer_catalogue") {
    sentence = "Sign-ins request every scope the identity provider advertises.";
  } else {
    const shown = shownScopes(client);
    const phrase = SOURCES[client.scopeSource]?.phrase;
    if (shown.length === 0 || !phrase) return null;
    sentence = `Sign-ins request ${shown.join(", ")}, ${phrase}.`;
  }
  return mayChange(client, scopes) ? `${sentence} ${MAY_CHANGE}` : sentence;
}
