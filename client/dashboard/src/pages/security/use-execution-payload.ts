import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import type { RiskRevealPayloadResult } from "@gram/client/models/components/riskrevealpayloadresult.js";
import { useRiskRevealResultPayloadMutation } from "@gram/client/react-query/riskRevealResultPayload.js";
import { useCallback, useState } from "react";

// Payload storage is per execution and phase; siblings in the same phase share
// one payload and their offsets index into it.
function payloadKey(result: RiskResult): string {
  return `${result.executionId ?? result.id}:${result.phase ?? ""}`;
}

export type ExecutionPayload = {
  data: RiskRevealPayloadResult | undefined;
  isPending: boolean;
  isError: boolean;
  /** Fetches the payload unless it is cached or already loading. */
  reveal: () => void;
};

// Reveals the full MCP payload for a finding through the audited endpoint and
// caches it per execution, so switching between sibling findings reuses it.
export function useExecutionPayload(
  result: RiskResult | null,
): ExecutionPayload {
  const { mutate, isPending, isError } = useRiskRevealResultPayloadMutation();
  const [cache, setCache] = useState<Map<string, RiskRevealPayloadResult>>(
    () => new Map(),
  );
  // Keyed by payload, so a sibling sharing it sees the request as its own.
  const [requestKey, setRequestKey] = useState<string | null>(null);
  const key = result ? payloadKey(result) : null;
  const isOwnRequest = key !== null && requestKey === key;
  const data = key ? cache.get(key) : undefined;

  const reveal = useCallback(() => {
    if (!result || !key || cache.has(key) || isPending) return;
    setRequestKey(key);
    mutate(
      { request: { riskIDRequestBody: { id: result.id } } },
      {
        onSuccess: (res) => setCache((prev) => new Map(prev).set(key, res)),
      },
    );
  }, [result, key, cache, isPending, mutate]);

  return {
    data,
    isPending: isPending && isOwnRequest,
    isError: isError && isOwnRequest && !data,
    reveal,
  };
}
