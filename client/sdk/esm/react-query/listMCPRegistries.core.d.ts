import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRegistriesResponseBody } from "../models/components/listregistriesresponsebody.js";
import {
  ListMCPRegistriesRequest,
  ListMCPRegistriesSecurity,
} from "../models/operations/listmcpregistries.js";
export type ListMCPRegistriesQueryData = ListRegistriesResponseBody;
export declare function prefetchListMCPRegistries(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListMCPRegistriesRequest | undefined,
  security?: ListMCPRegistriesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListMCPRegistriesQuery(
  client$: GramCore,
  request?: ListMCPRegistriesRequest | undefined,
  security?: ListMCPRegistriesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListMCPRegistriesQueryData>;
};
export declare function queryKeyListMCPRegistries(parameters: {
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listMCPRegistries.core.d.ts.map
