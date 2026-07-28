import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListPluginsResult } from "../models/components/listpluginsresult.js";
import {
  ListPluginsRequest,
  ListPluginsSecurity,
} from "../models/operations/listplugins.js";
export type PluginsQueryData = ListPluginsResult;
export declare function prefetchPlugins(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListPluginsRequest | undefined,
  security?: ListPluginsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildPluginsQuery(
  client$: GramCore,
  request?: ListPluginsRequest | undefined,
  security?: ListPluginsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<PluginsQueryData>;
};
export declare function queryKeyPlugins(parameters: {
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=plugins.core.d.ts.map
