import { formatRemoteSessionIssuerDisplay } from "@/lib/sources";

// issuerDisplayName is the primary label for a remote identity provider. It
// delegates to the centralized AIS-118 helper (display name → issuer URL) so the
// org-admin UI stays in sync with the MCP Server Authentication tab. The slug
// remains visible as a secondary identifier in the listing.
export function issuerDisplayName(issuer: {
  name?: string | null | undefined;
  issuer: string;
}): string {
  return formatRemoteSessionIssuerDisplay(issuer);
}
