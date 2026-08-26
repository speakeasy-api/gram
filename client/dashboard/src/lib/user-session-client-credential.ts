import type { CredentialKind } from "@gram/client/models/components/usersessionclient.js";

// What a registered client must present to authenticate. INBOUND: this server
// is the authorization server and the client is a third-party agent connecting
// to one of its MCP servers.
//
// The value is derived on the server by the same rule the token endpoint
// enforces, so it says what the client will actually be held to rather than
// what it declared. Never re-derive it here: the inputs include whether the row
// stores a client secret, which the management API deliberately never returns.
export type { CredentialKind };

// Whether the list badges a kind at all.
//
// Public is the overwhelming majority of registrations and secret is the RFC
// 7591 default, so badging either puts a chip on nearly every row of a roster
// whose whole design is that a healthy one stays quiet. The two exceptions earn
// the space: a key-authenticated client is the strongest posture available and
// worth seeing, and a misconfigured one cannot authenticate at all.
//
// The detail sheet states all four regardless, so nothing is only inferable
// from the absence of a badge.
const BADGED_CREDENTIAL_KINDS: readonly CredentialKind[] = [
  "key",
  "misconfigured",
];

export function isBadgedCredentialKind(
  kind: CredentialKind | undefined,
): kind is CredentialKind {
  return kind !== undefined && BADGED_CREDENTIAL_KINDS.includes(kind);
}

// Badge variant names are hooks onto the brand palette rather than literal
// alert semantics. Only misconfigured is an alert; key is a positive state
// wearing a quiet color so a roster of well-behaved clients does not read as a
// wall of warnings.
//
// Each `detail` completes a sentence that opens with the protocol value the
// client declared, so the exact term an operator would search for or quote in a
// support thread leads the tooltip instead of trailing it.
export const CREDENTIAL_KIND_PRESENTATION: Record<
  CredentialKind,
  {
    label: string;
    detail: string;
    badgeVariant: "success" | "information" | "neutral" | "destructive";
  }
> = {
  public: {
    label: "Public",
    badgeVariant: "neutral",
    detail: "presents no client credential; PKCE binds the code exchange.",
  },
  secret: {
    label: "Secret",
    badgeVariant: "information",
    detail: "presents a client secret Speakeasy issued at registration.",
  },
  key: {
    label: "Signed",
    badgeVariant: "success",
    detail:
      "proves identity by signing with a published key, so Speakeasy stores no shared secret. The strongest option a client can choose.",
  },
  misconfigured: {
    label: "Cannot authenticate",
    badgeVariant: "destructive",
    // The only kind whose tooltip has to leave the operator with a move. The
    // registration is the client's to fix, and revoking is what forces a fresh
    // one -- an action the same row offers.
    detail:
      "misconfigured. The stored credentials do not match this method, so no session can be issued or refreshed. Revoke the client to force reconfiguration.",
  },
};

// The declared value on its own, for a surface that already labels what it is
// showing. A client registered before the value was recorded declared nothing,
// which is said outright rather than shown as a blank: it is the reason the kind
// was read off the rest of the registration.
export function declaredAuthMethodValue(
  declaredMethod: string | null | undefined,
): string {
  return declaredMethod ?? "Not declared";
}

// Names the value as a method rather than leading with it bare: on its own,
// "none" or "private_key_jwt" reads as a fragment, and the tooltip has to say
// what kind of thing the value is before it says what it means.
export function credentialKindTooltip(
  kind: CredentialKind,
  declaredMethod: string | null | undefined,
): string {
  const declared = declaredMethod
    ? `Auth method ${declaredMethod}`
    : "No auth method declared";
  return `${declared}: ${CREDENTIAL_KIND_PRESENTATION[kind].detail}`;
}
