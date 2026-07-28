import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Plugin } from "../models/components/plugin.js";
import {
  GetPluginRequest,
  GetPluginSecurity,
} from "../models/operations/getplugin.js";
export type PluginQueryData = Plugin;
export declare function prefetchPlugin(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetPluginRequest,
  security?: GetPluginSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildPluginQuery(
  client$: GramCore,
  request: GetPluginRequest,
  security?: GetPluginSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<PluginQueryData>;
};
export declare function queryKeyPlugin(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=plugin.core.d.ts.map
