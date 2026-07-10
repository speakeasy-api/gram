import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { QueryResult } from "../models/components/queryresult.js";
import { QueryRequest, QuerySecurity } from "../models/operations/query.js";
export type TelemetryQueryQueryData = QueryResult;
export declare function prefetchTelemetryQuery(
  queryClient: QueryClient,
  client$: GramCore,
  request: QueryRequest,
  security?: QuerySecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildTelemetryQueryQuery(
  client$: GramCore,
  request: QueryRequest,
  security?: QuerySecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<TelemetryQueryQueryData>;
};
export declare function queryKeyTelemetryQuery(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=telemetryQuery.core.d.ts.map
