import { useCallback, useEffect, useState } from "react";

import type { XaaServerReadiness } from "@gram/client/models/components/xaaserverreadiness.js";

import type { AppInstanceOption } from "./xaaView";

/** Confirmations are shared only by servers with the same issuer ID and resource. */
export function sharedConfirmationKey(row: XaaServerReadiness): string {
  return row.issuerId
    ? JSON.stringify([row.issuerId, row.resourceIndicator])
    : JSON.stringify([row.mcpServerId]);
}

export type ClearedSnapshots = {
  snapshots: XaaServerReadiness[];
  snapshotFor: (mcpServerId: string) => XaaServerReadiness | undefined;
  remember: (row: XaaServerReadiness) => void;
  retire: (confirmed: XaaServerReadiness[]) => void;
  canUndo: (snapshot: XaaServerReadiness) => boolean;
};

/** Snapshots of cleared confirmations, kept until the shared confirmation is saved again. */
export function useClearedConfirmations(
  readiness: {
    servers: XaaServerReadiness[] | undefined;
    isPlaceholderData: boolean;
  },
  appInstances: AppInstanceOption[],
): ClearedSnapshots {
  const [snapshots, setSnapshots] = useState<XaaServerReadiness[]>([]);

  const retire = useCallback((confirmed: XaaServerReadiness[]) => {
    const keys = new Set(confirmed.map(sharedConfirmationKey));
    setSnapshots((previous) => {
      const remaining = previous.filter(
        (snapshot) => !keys.has(sharedConfirmationKey(snapshot)),
      );
      return remaining.length === previous.length ? previous : remaining;
    });
  }, []);

  const { servers, isPlaceholderData } = readiness;
  useEffect(() => {
    if (isPlaceholderData) return;
    retire((servers ?? []).filter((row) => row.state === "connected"));
  }, [servers, isPlaceholderData, retire]);

  const remember = useCallback(
    (row: XaaServerReadiness) =>
      setSnapshots((previous) => [
        ...previous.filter(
          (snapshot) => snapshot.mcpServerId !== row.mcpServerId,
        ),
        row,
      ]),
    [],
  );

  return {
    snapshots,
    snapshotFor: (mcpServerId) =>
      snapshots.find((snapshot) => snapshot.mcpServerId === mcpServerId),
    remember,
    retire,
    canUndo: (snapshot) =>
      !!snapshot.audience &&
      (!snapshot.oktaApplicationId ||
        appInstances.some((app) => app.id === snapshot.oktaApplicationId)),
  };
}
