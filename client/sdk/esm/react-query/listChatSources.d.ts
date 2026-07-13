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
  ListChatSourcesRequest,
  ListChatSourcesSecurity,
} from "../models/operations/listchatsources.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListChatSourcesQuery,
  ListChatSourcesQueryData,
  prefetchListChatSources,
  queryKeyListChatSources,
} from "./listChatSources.core.js";
export {
  buildListChatSourcesQuery,
  type ListChatSourcesQueryData,
  prefetchListChatSources,
  queryKeyListChatSources,
};
export type ListChatSourcesQueryError =
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
 * listSources chat
 *
 * @remarks
 * List the distinct agent sources present in this project's chats, for populating the agent-type filter on the Agent Sessions page.
 */
export declare function useListChatSources(
  request?: ListChatSourcesRequest | undefined,
  security?: ListChatSourcesSecurity | undefined,
  options?: QueryHookOptions<
    ListChatSourcesQueryData,
    ListChatSourcesQueryError
  >,
): UseQueryResult<ListChatSourcesQueryData, ListChatSourcesQueryError>;
/**
 * listSources chat
 *
 * @remarks
 * List the distinct agent sources present in this project's chats, for populating the agent-type filter on the Agent Sessions page.
 */
export declare function useListChatSourcesSuspense(
  request?: ListChatSourcesRequest | undefined,
  security?: ListChatSourcesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListChatSourcesQueryData,
    ListChatSourcesQueryError
  >,
): UseSuspenseQueryResult<ListChatSourcesQueryData, ListChatSourcesQueryError>;
export declare function setListChatSourcesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
      gramChatSession?: string | undefined;
    },
  ],
  data: ListChatSourcesQueryData,
): ListChatSourcesQueryData | undefined;
export declare function invalidateListChatSources(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
        gramChatSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListChatSources(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listChatSources.d.ts.map
