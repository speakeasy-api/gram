import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type OtelForwardingDestinationQueryData = components.OtelForwardingDestination;
export declare function prefetchOtelForwardingDestination(queryClient: QueryClient, client$: GramCore, request: operations.GetOtelForwardingDestinationRequest, security?: operations.GetOtelForwardingDestinationSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildOtelForwardingDestinationQuery(client$: GramCore, request: operations.GetOtelForwardingDestinationRequest, security?: operations.GetOtelForwardingDestinationSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<OtelForwardingDestinationQueryData>;
};
export declare function queryKeyOtelForwardingDestination(parameters: {
    id: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=otelForwardingDestination.core.d.ts.map