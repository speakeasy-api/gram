import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  SearchChatsRequest,
  SearchChatsSecurity,
} from "../models/operations/searchchats.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildSearchChatsQuery,
  prefetchSearchChats,
  queryKeySearchChats,
  SearchChatsQueryData,
} from "./searchChats.core.js";
export {
  buildSearchChatsQuery,
  prefetchSearchChats,
  queryKeySearchChats,
  type SearchChatsQueryData,
};
export type SearchChatsQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * searchChats telemetry
 *
 * @remarks
 * Search and list chat session summaries that match a search filter
 */
export declare function useSearchChats(
  request: SearchChatsRequest,
  security?: SearchChatsSecurity | undefined,
  options?: QueryHookOptions<SearchChatsQueryData, SearchChatsQueryError>,
): UseQueryResult<SearchChatsQueryData, SearchChatsQueryError>;
/**
 * searchChats telemetry
 *
 * @remarks
 * Search and list chat session summaries that match a search filter
 */
export declare function useSearchChatsSuspense(
  request: SearchChatsRequest,
  security?: SearchChatsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    SearchChatsQueryData,
    SearchChatsQueryError
  >,
): UseSuspenseQueryResult<SearchChatsQueryData, SearchChatsQueryError>;
export declare function setSearchChatsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: SearchChatsQueryData,
): SearchChatsQueryData | undefined;
export declare function invalidateSearchChats(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllSearchChats(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=searchChats.d.ts.map
