import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import type { IdentityMode } from "./identity";

/**
 * What the identity rules say when a header row breaks one. Grouped so the
 * copy lives in one place and callers can assert on a symbol rather than
 * repeating the sentence.
 */
export const IdentityErrors = {
  /** No Identity is selected, so nothing may set a static Authorization. */
  NoAuthorization:
    "Switch to Agent Identity to use a static Authorization credential.",
  /** User or Agent Identity owns the row, so a hand-written one may not. */
  ManagedAuthorization: "Authorization is managed in the Identity section.",
} as const;

export type IdentityError =
  (typeof IdentityErrors)[keyof typeof IdentityErrors];

/** Header names are case-insensitive, and operators type them by hand. */
function isAuthorizationHeader(name: string): boolean {
  return name.trim().toLowerCase() === "authorization";
}

/** The Authorization header the Agent Identity credential writes. */
export function findStaticAuthorizationHeader(
  headers: readonly RemoteMcpServerHeader[],
): RemoteMcpServerHeader | undefined {
  return headers.find(
    (header) =>
      isAuthorizationHeader(header.name) &&
      !!header.value &&
      !header.valueFromRequestHeader,
  );
}

/** The legacy row that forwards an inbound Authorization straight through. */
export function findPassThroughAuthorizationHeader(
  headers: readonly RemoteMcpServerHeader[],
): RemoteMcpServerHeader | undefined {
  return headers.find(
    (header) =>
      isAuthorizationHeader(header.name) && !!header.valueFromRequestHeader,
  );
}

/**
 * The rule the identity choice imposes on the header list: it owns the
 * Authorization name, so a hand-written row may not claim it.
 *
 * Returns the message to show, or null when the name is allowed.
 */
export function authorizationHeaderGuard(
  mode: IdentityMode,
  headerName: string,
  isManagedAuthorizationRow: boolean,
): IdentityError | null {
  if (!isAuthorizationHeader(headerName)) return null;
  if (mode === "none") return IdentityErrors.NoAuthorization;
  if (isManagedAuthorizationRow) return null;
  return IdentityErrors.ManagedAuthorization;
}

/**
 * The Authorization row the identity choice owns.
 *
 * The identity section publishes this; the header list consumes it. Before
 * this existed the header list re-derived the mode and re-found the row for
 * itself, which meant two places computing the same answer from two separate
 * fetches of the same data — and nothing guaranteeing they agreed.
 */
export type ManagedHeader = {
  readonly ownedBy: "user" | "agent";
  /** Null when the mode owns the name but no row exists yet. */
  readonly headerId: string | null;
};

export function managedAuthorizationHeader(
  mode: IdentityMode,
  headers: readonly RemoteMcpServerHeader[],
): ManagedHeader | null {
  if (mode === "none") return null;
  return {
    ownedBy: mode,
    headerId: findStaticAuthorizationHeader(headers)?.id ?? null,
  };
}
