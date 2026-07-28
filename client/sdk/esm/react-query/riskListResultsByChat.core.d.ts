import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsByChatResult } from "../models/components/listriskresultsbychatresult.js";
import {
  ListRiskResultsByChatRequest,
  ListRiskResultsByChatSecurity,
} from "../models/operations/listriskresultsbychat.js";
export type RiskListResultsByChatQueryData = ListRiskResultsByChatResult;
export declare function prefetchRiskListResultsByChat(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRiskResultsByChatRequest | undefined,
  security?: ListRiskResultsByChatSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskListResultsByChatQuery(
  client$: GramCore,
  request?: ListRiskResultsByChatRequest | undefined,
  security?: ListRiskResultsByChatSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskListResultsByChatQueryData>;
};
export declare function queryKeyRiskListResultsByChat(parameters: {
  cursor?: string | undefined;
  limit?: number | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListResultsByChat.core.d.ts.map
