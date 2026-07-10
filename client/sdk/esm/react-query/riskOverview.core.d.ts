import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskOverviewResult } from "../models/components/riskoverviewresult.js";
import { GetRiskOverviewRequest, GetRiskOverviewSecurity } from "../models/operations/getriskoverview.js";
export type RiskOverviewQueryData = RiskOverviewResult;
export declare function prefetchRiskOverview(queryClient: QueryClient, client$: GramCore, request?: GetRiskOverviewRequest | undefined, security?: GetRiskOverviewSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskOverviewQuery(client$: GramCore, request?: GetRiskOverviewRequest | undefined, security?: GetRiskOverviewSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskOverviewQueryData>;
};
export declare function queryKeyRiskOverview(parameters: {
    from?: Date | undefined;
    to?: Date | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskOverview.core.d.ts.map