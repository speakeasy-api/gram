import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type RiskListShadowMCPApprovalsQueryData =
  components.ListShadowMCPApprovalsResult;
export declare function prefetchRiskListShadowMCPApprovals(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.ListShadowMCPApprovalsRequest,
  security?: operations.ListShadowMCPApprovalsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskListShadowMCPApprovalsQuery(
  client$: GramCore,
  request: operations.ListShadowMCPApprovalsRequest,
  security?: operations.ListShadowMCPApprovalsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskListShadowMCPApprovalsQueryData>;
};
export declare function queryKeyRiskListShadowMCPApprovals(parameters: {
  policyId: string;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListShadowMCPApprovals.core.d.ts.map
