import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListResponseBody } from "../models/components/listresponsebody.js";
import {
  ListCollectionsRequest,
  ListCollectionsSecurity,
} from "../models/operations/listcollections.js";
export type ListCollectionsQueryData = ListResponseBody;
export declare function prefetchListCollections(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListCollectionsRequest | undefined,
  security?: ListCollectionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListCollectionsQuery(
  client$: GramCore,
  request?: ListCollectionsRequest | undefined,
  security?: ListCollectionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListCollectionsQueryData>;
};
export declare function queryKeyListCollections(parameters: {
  gramSession?: string | undefined;
  gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listCollections.core.d.ts.map
