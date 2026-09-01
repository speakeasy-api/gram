/**
 * The identifier a surface holds for a person, in the form the identity
 * resolver expects. Every surface passes whichever one it has — the resolver
 * folds them onto the same canonical URN, so all of them land on one page.
 */
export type IdentityRef =
  | { userId: string }
  | { email: string }
  | { externalUserId: string }
  // Some surfaces already hold a URN of their own (a session subject, an authz
  // principal); those pass it through rather than taking it apart first.
  | { urn: string };

/**
 * The identifier a telemetry row carries, keyed by the kind the row reports.
 * Rows whose kind is unknown have nothing the resolver can key on.
 */
export function identityRefForKind(
  kind: string,
  value: string,
): IdentityRef | null {
  if (!value) return null;
  switch (kind) {
    case "email":
      return { email: value };
    case "user_id":
      return { userId: value };
    case "external_user_id":
      return { externalUserId: value };
    default:
      return null;
  }
}

/**
 * A surface that records one opaque "user key" per row — an address for some
 * agents, an agent-side id for others — and does not say which it holds.
 */
export function identityRefForUserKey(
  key: string | null | undefined,
): IdentityRef | null {
  if (!key) return null;
  return key.includes("@") ? { email: key } : { externalUserId: key };
}

export function identityUrnFor(ref: IdentityRef): string {
  if ("urn" in ref) return ref.urn;
  if ("userId" in ref) return `user:${ref.userId}`;
  if ("email" in ref) return `email:${ref.email}`;
  return `external:${ref.externalUserId}`;
}

/**
 * URNs carry a colon (`user:user_01h8x`), which is legal in a path segment but
 * ambiguous once a caller pastes it, so links encode and pages decode.
 */
export function encodeIdentityUrn(urn: string): string {
  return encodeURIComponent(urn);
}
