import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OtelForwardingConfig } from "../models/components/otelforwardingconfig.js";
import {
  GetOtelForwardingConfigRequest,
  GetOtelForwardingConfigSecurity,
} from "../models/operations/getotelforwardingconfig.js";
export type OtelForwardingConfigQueryData = OtelForwardingConfig;
export declare function prefetchOtelForwardingConfig(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetOtelForwardingConfigRequest | undefined,
  security?: GetOtelForwardingConfigSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildOtelForwardingConfigQuery(
  client$: GramCore,
  request?: GetOtelForwardingConfigRequest | undefined,
  security?: GetOtelForwardingConfigSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<OtelForwardingConfigQueryData>;
};
export declare function queryKeyOtelForwardingConfig(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=otelForwardingConfig.core.d.ts.map
