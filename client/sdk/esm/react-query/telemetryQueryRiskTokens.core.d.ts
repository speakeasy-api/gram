import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { QueryRiskTokensResult } from "../models/components/queryrisktokensresult.js";
import { QueryRiskTokensRequest, QueryRiskTokensSecurity } from "../models/operations/queryrisktokens.js";
export type TelemetryQueryRiskTokensQueryData = QueryRiskTokensResult;
export declare function prefetchTelemetryQueryRiskTokens(queryClient: QueryClient, client$: GramCore, request: QueryRiskTokensRequest, security?: QueryRiskTokensSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildTelemetryQueryRiskTokensQuery(client$: GramCore, request: QueryRiskTokensRequest, security?: QueryRiskTokensSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<TelemetryQueryRiskTokensQueryData>;
};
export declare function queryKeyTelemetryQueryRiskTokens(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=telemetryQueryRiskTokens.core.d.ts.map