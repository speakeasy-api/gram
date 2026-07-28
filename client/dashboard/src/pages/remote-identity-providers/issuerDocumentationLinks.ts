import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";

export type IssuerDocumentationLink = {
  label: string;
  url: string;
};

// Whether a URL is safe to place in an href. The server validates every
// documentation URL on write, but serviceDocumentation originates from an
// upstream issuer's metadata document, and react-router hands any scheme it
// recognizes straight to the DOM — so a `javascript:` URL that ever slipped
// past a write path would execute on click. Checking again at the render sink
// keeps that from depending on an invariant held one layer away.
export function isAbsoluteHttpUrl(raw: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    return false;
  }

  return parsed.protocol === "http:" || parsed.protocol === "https:";
}

// The documentation URLs an issuer can carry, in the order they are shown.
// Entries whose URL is unset or blank are omitted, so an issuer with no
// documentation yields an empty list and its callers render nothing.
//
// The operator-supplied client setup URL comes first: it is the one written for
// customers standing up their own OAuth client. serviceDocumentation is the
// issuer's own RFC 8414 developer documentation. op_policy_uri and op_tos_uri
// are stored but deliberately not surfaced.
//
// Entries that are not absolute http(s) URLs are dropped rather than linked.
export function issuerDocumentationLinks(
  issuer: RemoteSessionIssuer,
): IssuerDocumentationLink[] {
  const candidates: Array<{ label: string; url: string | undefined }> = [
    {
      label: "Client Setup Documentation",
      url: issuer.clientSetupDocumentationUrl,
    },
    { label: "Service Documentation", url: issuer.serviceDocumentation },
  ];

  return candidates.flatMap(({ label, url }) => {
    const trimmed = url?.trim();
    return trimmed && isAbsoluteHttpUrl(trimmed)
      ? [{ label, url: trimmed }]
      : [];
  });
}
