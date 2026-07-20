import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyStatus } from "../models/components/riskpolicystatus.js";
import {
  GetRiskPolicyStatusRequest,
  GetRiskPolicyStatusSecurity,
} from "../models/operations/getriskpolicystatus.js";
export type RiskPoliciesStatusQueryData = RiskPolicyStatus;
export declare function prefetchRiskPoliciesStatus(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetRiskPolicyStatusRequest,
  security?: GetRiskPolicyStatusSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskPoliciesStatusQuery(
  client$: GramCore,
  request: GetRiskPolicyStatusRequest,
  security?: GetRiskPolicyStatusSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskPoliciesStatusQueryData>;
};
export declare function queryKeyRiskPoliciesStatus(parameters: {
  id: string;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskPoliciesStatus.core.d.ts.map
