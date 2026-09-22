import type { RemoteSession } from "@gram/client/models/components/remotesession.js";

/** Stored upstream identity only. Gram subject identity is not the account. */
export function sessionAccountIdentity(session?: RemoteSession): {
  displayName: string | undefined;
  email: string | undefined;
} {
  const displayName = session?.upstreamDisplayName?.trim() || undefined;
  const email = session?.upstreamEmail?.trim() || undefined;
  return { displayName, email };
}

export function sessionAccountLabel(session?: RemoteSession): string {
  const { displayName, email } = sessionAccountIdentity(session);
  return (
    [displayName, email].filter(Boolean).join(" · ") || "Identity unavailable"
  );
}
