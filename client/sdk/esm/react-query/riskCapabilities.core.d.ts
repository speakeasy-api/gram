import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type RiskCapabilitiesQueryData = components.RiskCapabilitiesResult;
export declare function prefetchRiskCapabilities(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.GetRiskCapabilitiesRequest | undefined,
  security?: operations.GetRiskCapabilitiesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRiskCapabilitiesQuery(
  client$: GramCore,
  request?: operations.GetRiskCapabilitiesRequest | undefined,
  security?: operations.GetRiskCapabilitiesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RiskCapabilitiesQueryData>;
};
export declare function queryKeyRiskCapabilities(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskCapabilities.core.d.ts.map
