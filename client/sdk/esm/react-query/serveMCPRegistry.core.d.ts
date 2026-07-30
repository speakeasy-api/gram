import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ServeMCPRegistryQueryData = components.ServeResponseBody;
export declare function prefetchServeMCPRegistry(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.ServeMCPRegistryRequest,
  security?: operations.ServeMCPRegistrySecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildServeMCPRegistryQuery(
  client$: GramCore,
  request: operations.ServeMCPRegistryRequest,
  security?: operations.ServeMCPRegistrySecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ServeMCPRegistryQueryData>;
};
export declare function queryKeyServeMCPRegistry(parameters: {
  registrySlug: string;
  search?: string | undefined;
  cursor?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=serveMCPRegistry.core.d.ts.map
