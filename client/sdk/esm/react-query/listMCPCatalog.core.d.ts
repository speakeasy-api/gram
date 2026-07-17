import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListCatalogResponseBody } from "../models/components/listcatalogresponsebody.js";
import {
  ListMCPCatalogRequest,
  ListMCPCatalogSecurity,
} from "../models/operations/listmcpcatalog.js";
export type ListMCPCatalogQueryData = ListCatalogResponseBody;
export declare function prefetchListMCPCatalog(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListMCPCatalogRequest | undefined,
  security?: ListMCPCatalogSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListMCPCatalogQuery(
  client$: GramCore,
  request?: ListMCPCatalogRequest | undefined,
  security?: ListMCPCatalogSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListMCPCatalogQueryData>;
};
export declare function queryKeyListMCPCatalog(parameters: {
  registryId?: string | undefined;
  search?: string | undefined;
  cursor?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listMCPCatalog.core.d.ts.map
