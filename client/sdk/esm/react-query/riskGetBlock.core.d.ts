import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskBlock } from "../models/components/riskblock.js";
import {
  GetRiskBlockRequest,
  GetRiskBlockSecurity,
} from "../models/operations/getriskblock.js";
export type RiskGetBlockQueryData = RiskBlock;
export declare function prefetchRiskGetBlock(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetRiskBlockRequest,
  security?: GetRiskBlockSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskGetBlockQuery(
  client$: GramCore,
  request: GetRiskBlockRequest,
  security?: GetRiskBlockSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<RiskGetBlockQueryData>;
};
export declare function queryKeyRiskGetBlock(parameters: {
  id: string;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskGetBlock.core.d.ts.map
