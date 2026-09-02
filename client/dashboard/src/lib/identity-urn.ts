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
 * Whether a value is the bare address an `email:` identity is keyed on. The
 * resolver rejects anything else outright, so a surface that reports freeform
 * values (an MDM's assigned user, an agent-side handle) has to check before it
 * mints one rather than linking to a URN that cannot be parsed.
 */
export function isEmailAddress(value: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim());
}

/**
 * A surface that records one opaque "user key" per row — an address for some
 * agents, an agent-side id for others — and does not say which it holds.
 */
export function identityRefForUserKey(
  key: string | null | undefined,
): IdentityRef | null {
  if (!key) return null;
  return isEmailAddress(key) ? { email: key } : { externalUserId: key };
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

/**
 * The window keys the identity page reads off the URL.
 *
 * A reader who has narrowed a page to a period expects the person's page to
 * open on that same period; landing on the default window reads as the link
 * having filtered to something other than what they were looking at.
 */
const IDENTITY_WINDOW_PARAMS = ["range", "from", "to", "label"];

/** Carries the reader's current window onto an identity page href. */
export function withIdentityWindow(href: string, search: string): string {
  const current = new URLSearchParams(search);
  const window = new URLSearchParams();
  for (const key of IDENTITY_WINDOW_PARAMS) {
    const value = current.get(key);
    if (value) window.set(key, value);
  }
  const encoded = window.toString();
  return encoded ? `${href}?${encoded}` : href;
}
