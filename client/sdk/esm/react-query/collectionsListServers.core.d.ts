import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListServersResponseBody } from "../models/components/listserversresponsebody.js";
import {
  ListCollectionServersRequest,
  ListCollectionServersSecurity,
} from "../models/operations/listcollectionservers.js";
export type CollectionsListServersQueryData = ListServersResponseBody;
export declare function prefetchCollectionsListServers(
  queryClient: QueryClient,
  client$: GramCore,
  request: ListCollectionServersRequest,
  security?: ListCollectionServersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildCollectionsListServersQuery(
  client$: GramCore,
  request: ListCollectionServersRequest,
  security?: ListCollectionServersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<CollectionsListServersQueryData>;
};
export declare function queryKeyCollectionsListServers(parameters: {
  collectionSlug: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=collectionsListServers.core.d.ts.map
