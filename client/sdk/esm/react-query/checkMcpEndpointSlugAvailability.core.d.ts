import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  CheckMcpEndpointSlugAvailabilityRequest,
  CheckMcpEndpointSlugAvailabilitySecurity,
} from "../models/operations/checkmcpendpointslugavailability.js";
export type CheckMcpEndpointSlugAvailabilityQueryData = boolean;
export declare function prefetchCheckMcpEndpointSlugAvailability(
  queryClient: QueryClient,
  client$: GramCore,
  request: CheckMcpEndpointSlugAvailabilityRequest,
  security?: CheckMcpEndpointSlugAvailabilitySecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildCheckMcpEndpointSlugAvailabilityQuery(
  client$: GramCore,
  request: CheckMcpEndpointSlugAvailabilityRequest,
  security?: CheckMcpEndpointSlugAvailabilitySecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<CheckMcpEndpointSlugAvailabilityQueryData>;
};
export declare function queryKeyCheckMcpEndpointSlugAvailability(parameters: {
  slug: string;
  customDomainId?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=checkMcpEndpointSlugAvailability.core.d.ts.map
