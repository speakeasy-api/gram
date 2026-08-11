import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

// How a client came to be registered against a user-session issuer. This is
// INBOUND: Gram is the authorization server and the client is a third-party
// agent connecting to a Gram MCP server. Do not reuse the outbound
// CLIENT_TYPE_LABELS from issuerFormUtils — those describe the mirror-image
// case where Gram is the OAuth client and Gram hosts the metadata document.
export type UserSessionClientSource = "cimd" | "dcr";

// Anything carrying the discriminator: both UserSessionClient and UserSession
// expose client_id_metadata_uri, so one helper serves the clients listing and
// the session listings alike.
type SourceBearing = Pick<
  UserSessionClient | UserSession,
  "clientIdMetadataUri"
>;

// The single place the CIMD/DCR distinction is derived. Never inline the
// null check at a call site — a third registration mode (a manual, operator-
// created client) is already plausible, and routing every consumer through
// here keeps that a one-line change rather than an N-site hunt.
export function userSessionClientSource(
  client: SourceBearing,
): UserSessionClientSource {
  return client.clientIdMetadataUri ? "cimd" : "dcr";
}

// Badge variant names are hooks onto the brand palette rather than literal
// alert semantics: these two are a visual distinction between registration
// modes, not a ranking of them.
export const SOURCE_PRESENTATION: Record<
  UserSessionClientSource,
  { label: string; tooltip: string; badgeVariant: "success" | "warning" }
> = {
  cimd: {
    label: "CIMD",
    badgeVariant: "success",
    tooltip:
      "Client ID Metadata Document. The client identified itself with a URL, and Gram fetched its OAuth metadata from that document instead of requiring it to register first. The document's origin is its identity.",
  },
  dcr: {
    label: "DCR",
    badgeVariant: "warning",
    tooltip:
      "Dynamic Client Registration (RFC 7591). The client registered with Gram up front and was issued a client_id.",
  },
};

// The origin of the metadata document, which is what actually identifies a
// CIMD client. Returns null for DCR clients and for anything unparseable.
//
// Prefer this over client_name in any listing: client_name is chosen by the
// client itself and verified by nobody, so a hostile client can register as
// "Claude Code". The origin cannot be spoofed — Gram fetched the document
// from it.
export function clientDocumentOrigin(client: SourceBearing): string | null {
  if (!client.clientIdMetadataUri) return null;
  try {
    return new URL(client.clientIdMetadataUri).origin;
  } catch {
    return null;
  }
}
