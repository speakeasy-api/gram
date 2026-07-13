import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TumDetailsResult } from "../models/components/tumdetailsresult.js";
import { QueryTumDetailsRequest, QueryTumDetailsSecurity } from "../models/operations/querytumdetails.js";
export type TelemetryQueryTumDetailsQueryData = TumDetailsResult;
export declare function prefetchTelemetryQueryTumDetails(queryClient: QueryClient, client$: GramCore, request: QueryTumDetailsRequest, security?: QueryTumDetailsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildTelemetryQueryTumDetailsQuery(client$: GramCore, request: QueryTumDetailsRequest, security?: QueryTumDetailsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<TelemetryQueryTumDetailsQueryData>;
};
export declare function queryKeyTelemetryQueryTumDetails(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=telemetryQueryTumDetails.core.d.ts.map