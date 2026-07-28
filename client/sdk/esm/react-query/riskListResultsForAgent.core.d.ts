import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsForAgentResult } from "../models/components/listriskresultsforagentresult.js";
import {
  ListRiskResultsForAgentRequest,
  ListRiskResultsForAgentSecurity,
} from "../models/operations/listriskresultsforagent.js";
export type RiskListResultsForAgentQueryData = ListRiskResultsForAgentResult;
export declare function prefetchRiskListResultsForAgent(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListRiskResultsForAgentRequest | undefined,
  security?: ListRiskResultsForAgentSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskListResultsForAgentQuery(
  client$: GramCore,
  request?: ListRiskResultsForAgentRequest | undefined,
  security?: ListRiskResultsForAgentSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskListResultsForAgentQueryData>;
};
export declare function queryKeyRiskListResultsForAgent(parameters: {
  policyId?: string | undefined;
  chatId?: string | undefined;
  category?: string | undefined;
  ruleId?: string | undefined;
  userId?: string | undefined;
  uniqueMatch?: boolean | undefined;
  from?: Date | undefined;
  to?: Date | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListResultsForAgent.core.d.ts.map
