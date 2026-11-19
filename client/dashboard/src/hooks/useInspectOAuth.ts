import { useSession } from "@/contexts/Auth";
import {
  prepareInspectAuthorization,
  readInspectGrant,
} from "@/lib/inspect-oauth";
import { getServerURL, mcpConnectionUrl } from "@/lib/utils";
import { useEffect, useState } from "react";

export function useInspectOAuth({
  platformSlug,
  issuerId,
  serverId,
}: {
  platformSlug: string | undefined;
  issuerId: string | undefined;
  serverId: string | undefined;
}) {
  const session = useSession();
  const userId = session.user.id;
  const serverUrl = platformSlug
    ? `${getServerURL()}/mcp/${encodeURIComponent(platformSlug)}`
    : undefined;
  const grantKey =
    serverUrl && issuerId && serverId && userId
      ? `inspect-oauth:${JSON.stringify([session.organization.id, userId, issuerId, serverId, serverUrl])}`
      : undefined;
  const grant = readInspectGrant(grantKey);
  const [, expire] = useState(0);
  const [isConnecting, setConnecting] = useState(false);
  const [error, setError] = useState(false);
  const expiresAt = grant?.expiresAt;
  useEffect(() => {
    if (!expiresAt) return;
    const timer = window.setTimeout(
      () => expire((n) => n + 1),
      Math.max(0, expiresAt - Date.now()),
    );
    return () => window.clearTimeout(timer);
  }, [expiresAt]);

  const tokenUrl = serverUrl && mcpConnectionUrl(`${serverUrl}/token`);
  const connect =
    serverUrl && tokenUrl && grantKey
      ? () => {
          setConnecting(true);
          setError(false);
          void prepareInspectAuthorization({
            serverUrl,
            tokenUrl,
            grantKey,
            userId,
          })
            .then((authorize) => location.assign(authorize))
            .catch(() => {
              setConnecting(false);
              setError(true);
            });
        }
      : undefined;

  return {
    accessToken: grant?.accessToken,
    serverUrl,
    connect,
    isConnecting,
    error,
  };
}
