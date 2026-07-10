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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListChatsWithResolutionsQuery,
  ListChatsWithResolutionsQueryData,
  prefetchListChatsWithResolutions,
  queryKeyListChatsWithResolutions,
} from "./listChatsWithResolutions.core.js";
export {
  buildListChatsWithResolutionsQuery,
  type ListChatsWithResolutionsQueryData,
  prefetchListChatsWithResolutions,
  queryKeyListChatsWithResolutions,
};
export type ListChatsWithResolutionsQueryError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * listChatsWithResolutions chat
 *
 * @remarks
 * List all chats for a project with their resolutions
 */
export declare function useListChatsWithResolutions(
  request?: operations.ListChatsWithResolutionsRequest | undefined,
  security?: operations.ListChatsWithResolutionsSecurity | undefined,
  options?: QueryHookOptions<
    ListChatsWithResolutionsQueryData,
    ListChatsWithResolutionsQueryError
  >,
): UseQueryResult<
  ListChatsWithResolutionsQueryData,
  ListChatsWithResolutionsQueryError
>;
/**
 * listChatsWithResolutions chat
 *
 * @remarks
 * List all chats for a project with their resolutions
 */
export declare function useListChatsWithResolutionsSuspense(
  request?: operations.ListChatsWithResolutionsRequest | undefined,
  security?: operations.ListChatsWithResolutionsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ListChatsWithResolutionsQueryData,
    ListChatsWithResolutionsQueryError
  >,
): UseSuspenseQueryResult<
  ListChatsWithResolutionsQueryData,
  ListChatsWithResolutionsQueryError
>;
export declare function setListChatsWithResolutionsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      search?: string | undefined;
      externalUserId?: string | undefined;
      assistantId?: string | undefined;
      resolutionStatus?: string | undefined;
      hasRisk?: operations.HasRisk | undefined;
      from?: Date | undefined;
      to?: Date | undefined;
      limit?: number | undefined;
      offset?: number | undefined;
      sortBy?: operations.SortBy | undefined;
      sortOrder?: operations.SortOrder | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
      gramChatSession?: string | undefined;
    },
  ],
  data: ListChatsWithResolutionsQueryData,
): ListChatsWithResolutionsQueryData | undefined;
export declare function invalidateListChatsWithResolutions(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        search?: string | undefined;
        externalUserId?: string | undefined;
        assistantId?: string | undefined;
        resolutionStatus?: string | undefined;
        hasRisk?: operations.HasRisk | undefined;
        from?: Date | undefined;
        to?: Date | undefined;
        limit?: number | undefined;
        offset?: number | undefined;
        sortBy?: operations.SortBy | undefined;
        sortOrder?: operations.SortOrder | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
        gramChatSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListChatsWithResolutions(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listChatsWithResolutions.d.ts.map
