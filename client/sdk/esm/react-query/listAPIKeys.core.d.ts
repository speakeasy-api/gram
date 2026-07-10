import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListKeysResult } from "../models/components/listkeysresult.js";
import {
  ListAPIKeysRequest,
  ListAPIKeysSecurity,
} from "../models/operations/listapikeys.js";
export type ListAPIKeysQueryData = ListKeysResult;
export declare function prefetchListAPIKeys(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListAPIKeysRequest | undefined,
  security?: ListAPIKeysSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListAPIKeysQuery(
  client$: GramCore,
  request?: ListAPIKeysRequest | undefined,
  security?: ListAPIKeysSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListAPIKeysQueryData>;
};
export declare function queryKeyListAPIKeys(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listAPIKeys.core.d.ts.map
