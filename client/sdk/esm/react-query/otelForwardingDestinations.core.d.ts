import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type OtelForwardingDestinationsQueryData =
  components.OtelForwardingDestinationList;
export declare function prefetchOtelForwardingDestinations(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.ListOtelForwardingDestinationsRequest | undefined,
  security?: operations.ListOtelForwardingDestinationsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOtelForwardingDestinationsQuery(
  client$: GramCore,
  request?: operations.ListOtelForwardingDestinationsRequest | undefined,
  security?: operations.ListOtelForwardingDestinationsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<OtelForwardingDestinationsQueryData>;
};
export declare function queryKeyOtelForwardingDestinations(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=otelForwardingDestinations.core.d.ts.map
