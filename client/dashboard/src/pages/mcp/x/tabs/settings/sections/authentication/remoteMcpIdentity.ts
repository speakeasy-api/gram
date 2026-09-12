import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";

export type RemoteMcpIdentityMode = "user" | "agent" | "none";

export const NO_IDENTITY_AUTHORIZATION_ERROR =
  "Switch to Agent Identity to use a static Authorization credential.";

function isAuthorizationHeader(name: string): boolean {
  return name.trim().toLowerCase() === "authorization";
}

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

export function findPassThroughAuthorizationHeader(
  headers: readonly RemoteMcpServerHeader[],
): RemoteMcpServerHeader | undefined {
  return headers.find(
    (header) =>
      isAuthorizationHeader(header.name) && !!header.valueFromRequestHeader,
  );
}

export function deriveRemoteMcpIdentityMode(
  linkedClientCount: number,
  headers: readonly RemoteMcpServerHeader[],
): RemoteMcpIdentityMode {
  if (linkedClientCount > 0) return "user";

  if (findStaticAuthorizationHeader(headers)) return "agent";

  return "none";
}

export function authorizationHeaderGuard(
  mode: RemoteMcpIdentityMode,
  headerName: string,
  isManagedAuthorizationRow: boolean,
): string | null {
  if (!isAuthorizationHeader(headerName)) return null;
  if (mode === "none") return NO_IDENTITY_AUTHORIZATION_ERROR;
  if (isManagedAuthorizationRow) return null;
  return "Authorization is managed in the Identity section.";
}
