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
  AccountType,
  HasRisk,
  ListChatsRequest,
  ListChatsSecurity,
  Pinned,
  SortBy,
  SortOrder,
} from "../models/operations/listchats.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListChatsQuery,
  ListChatsQueryData,
  prefetchListChats,
  queryKeyListChats,
} from "./listChats.core.js";
export {
  buildListChatsQuery,
  type ListChatsQueryData,
  prefetchListChats,
  queryKeyListChats,
};
export type ListChatsQueryError =
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
 * listChats chat
 *
 * @remarks
 * List all chats for a project
 */
export declare function useListChats(
  request?: ListChatsRequest | undefined,
  security?: ListChatsSecurity | undefined,
  options?: QueryHookOptions<ListChatsQueryData, ListChatsQueryError>,
): UseQueryResult<ListChatsQueryData, ListChatsQueryError>;
/**
 * listChats chat
 *
 * @remarks
 * List all chats for a project
 */
export declare function useListChatsSuspense(
  request?: ListChatsRequest | undefined,
  security?: ListChatsSecurity | undefined,
  options?: SuspenseQueryHookOptions<ListChatsQueryData, ListChatsQueryError>,
): UseSuspenseQueryResult<ListChatsQueryData, ListChatsQueryError>;
export declare function setListChatsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      search?: string | undefined;
      externalUserId?: string | undefined;
      source?: string | undefined;
      assistantId?: string | undefined;
      sourceKind?: string | undefined;
      excludeSourceKind?: string | undefined;
      hasRisk?: HasRisk | undefined;
      accountType?: AccountType | undefined;
      pinned?: Pinned | undefined;
      minRiskScore?: number | undefined;
      from?: Date | undefined;
      to?: Date | undefined;
      limit?: number | undefined;
      offset?: number | undefined;
      sortBy?: SortBy | undefined;
      sortOrder?: SortOrder | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
      gramChatSession?: string | undefined;
    },
  ],
  data: ListChatsQueryData,
): ListChatsQueryData | undefined;
export declare function invalidateListChats(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        search?: string | undefined;
        externalUserId?: string | undefined;
        source?: string | undefined;
        assistantId?: string | undefined;
        sourceKind?: string | undefined;
        excludeSourceKind?: string | undefined;
        hasRisk?: HasRisk | undefined;
        accountType?: AccountType | undefined;
        pinned?: Pinned | undefined;
        minRiskScore?: number | undefined;
        from?: Date | undefined;
        to?: Date | undefined;
        limit?: number | undefined;
        offset?: number | undefined;
        sortBy?: SortBy | undefined;
        sortOrder?: SortOrder | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
        gramChatSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListChats(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listChats.d.ts.map
