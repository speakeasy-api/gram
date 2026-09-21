import { useCallback, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { XaaServerReadiness } from "@gram/client/models/components/xaaserverreadiness.js";
import { useConfirmXaaConnectionsMutation } from "@gram/client/react-query/confirmXaaConnections.js";

import {
  inlineError,
  invalidateIdentityProviderQueries,
  SESSION_SECURITY,
} from "../../identityProviderQueries";
import { buildConfirmRequests } from "./xaaView";

export type ConfirmFeedback =
  | { kind: "idle" }
  | { kind: "confirmed"; count: number }
  | { kind: "failed"; error: unknown; confirmedCount: number };

export type ConfirmOutcome =
  | { ok: true; count: number }
  | { ok: false; confirmedIds: Set<string> };

const IDLE: ConfirmFeedback = { kind: "idle" };

/** Confirms rows in API-sized batches; undefined means another confirmation was already running. */
export function useXaaConfirm(
  onBatchConfirmed: (rows: XaaServerReadiness[]) => void,
): {
  confirming: boolean;
  feedback: ConfirmFeedback;
  clearFeedback: () => void;
  confirmRows: (
    rows: XaaServerReadiness[],
    audience: string,
    oktaApplicationId: string | undefined,
  ) => Promise<ConfirmOutcome | undefined>;
} {
  const queryClient = useQueryClient();
  const confirm = useConfirmXaaConnectionsMutation({ onError: inlineError });
  const [confirming, setConfirming] = useState(false);
  const [feedback, setFeedback] = useState<ConfirmFeedback>(IDLE);
  const inFlight = useRef(false);

  const clearFeedback = useCallback(() => setFeedback(IDLE), []);

  const confirmRows = async (
    rows: XaaServerReadiness[],
    audience: string,
    oktaApplicationId: string | undefined,
  ): Promise<ConfirmOutcome | undefined> => {
    if (inFlight.current || rows.length === 0) return undefined;
    inFlight.current = true;
    setConfirming(true);
    setFeedback(IDLE);
    const confirmedIds = new Set<string>();
    const sentIds = new Set<string>();
    try {
      for (const body of buildConfirmRequests(
        rows,
        audience,
        oktaApplicationId,
      )) {
        const result = await confirm.mutateAsync({
          security: SESSION_SECURITY,
          request: { confirmXaaConnectionsRequestBody: body },
        });
        const batchIds = new Set(body.connections.map((c) => c.mcpServerId));
        onBatchConfirmed(rows.filter((row) => batchIds.has(row.mcpServerId)));
        for (const server of result.servers) {
          confirmedIds.add(server.mcpServerId);
        }
        for (const id of batchIds) sentIds.add(id);
      }
      setFeedback({ kind: "confirmed", count: confirmedIds.size });
      return { ok: true, count: confirmedIds.size };
    } catch (error) {
      setFeedback({
        kind: "failed",
        error,
        confirmedCount: confirmedIds.size,
      });
      return {
        ok: false,
        confirmedIds: new Set([...confirmedIds, ...sentIds]),
      };
    } finally {
      inFlight.current = false;
      setConfirming(false);
      void invalidateIdentityProviderQueries(queryClient);
    }
  };

  return { confirming, feedback, clearFeedback, confirmRows };
}
