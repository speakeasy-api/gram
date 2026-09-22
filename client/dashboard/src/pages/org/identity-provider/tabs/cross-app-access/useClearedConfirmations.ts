import { useCallback, useEffect, useState } from "react";

import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";

import type { AppInstanceOption } from "./xaaView";

/** Confirmations are shared only by servers with the same issuer ID and resource. */
export function sharedConfirmationKey(
  row: OktaResourceConnectionServer,
): string {
  return row.issuerId
    ? JSON.stringify([row.issuerId, row.resourceIndicator])
    : JSON.stringify([row.mcpServerId]);
}

export type ClearedSnapshots = {
  snapshots: OktaResourceConnectionServer[];
  snapshotFor: (
    mcpServerId: string,
  ) => OktaResourceConnectionServer | undefined;
  remember: (row: OktaResourceConnectionServer) => void;
  retire: (confirmed: OktaResourceConnectionServer[]) => void;
  canUndo: (snapshot: OktaResourceConnectionServer) => boolean;
};

/** Snapshots of cleared confirmations, kept until the shared confirmation is saved again. */
export function useClearedConfirmations(
  readiness: {
    servers: OktaResourceConnectionServer[] | undefined;
    isPlaceholderData: boolean;
  },
  appInstances: AppInstanceOption[],
): ClearedSnapshots {
  const [snapshots, setSnapshots] = useState<OktaResourceConnectionServer[]>(
    [],
  );

  const retire = useCallback((confirmed: OktaResourceConnectionServer[]) => {
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
    (row: OktaResourceConnectionServer) =>
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
