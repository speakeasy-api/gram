import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { findStaticAuthorizationHeader } from "./headers";

/**
 * How callers of an MCP server are identified to the upstream service. One
 * choice, three answers — see the module README for why the static-credential
 * and no-identity cases live beside the provider rather than apart from it.
 */
export type IdentityMode = "user" | "agent" | "none";

/**
 * Derived, never stored. A server reads as User because a remote session
 * client is bound to its user session issuer, and as Agent because a static
 * Authorization header exists.
 *
 * User wins when both are present: AIM-230 treats that as a legacy
 * configuration to be shown for cleanup, not an error, which is why switching
 * from Agent to User is a legal move.
 */
export function deriveIdentityMode(
  linkedClientCount: number,
  headers: readonly RemoteMcpServerHeader[],
): IdentityMode {
  if (linkedClientCount > 0) return "user";
  if (findStaticAuthorizationHeader(headers)) return "agent";
  return "none";
}
