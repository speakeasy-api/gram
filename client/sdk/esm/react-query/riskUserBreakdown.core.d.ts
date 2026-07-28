import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskUserBreakdownResult } from "../models/components/riskuserbreakdownresult.js";
import {
  GetRiskUserBreakdownRequest,
  GetRiskUserBreakdownSecurity,
} from "../models/operations/getriskuserbreakdown.js";
export type RiskUserBreakdownQueryData = RiskUserBreakdownResult;
export declare function prefetchRiskUserBreakdown(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetRiskUserBreakdownRequest,
  security?: GetRiskUserBreakdownSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskUserBreakdownQuery(
  client$: GramCore,
  request: GetRiskUserBreakdownRequest,
  security?: GetRiskUserBreakdownSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskUserBreakdownQueryData>;
};
export declare function queryKeyRiskUserBreakdown(parameters: {
  externalUserId: string;
  from?: Date | undefined;
  to?: Date | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskUserBreakdown.core.d.ts.map
