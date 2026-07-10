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
  SearchUsersRequest,
  SearchUsersSecurity,
} from "../models/operations/searchusers.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildSearchUsersQuery,
  prefetchSearchUsers,
  queryKeySearchUsers,
  SearchUsersQueryData,
} from "./searchUsers.core.js";
export {
  buildSearchUsersQuery,
  prefetchSearchUsers,
  queryKeySearchUsers,
  type SearchUsersQueryData,
};
export type SearchUsersQueryError =
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
 * searchUsers telemetry
 *
 * @remarks
 * Search and list user usage summaries grouped by user_id or external_user_id
 */
export declare function useSearchUsers(
  request: SearchUsersRequest,
  security?: SearchUsersSecurity | undefined,
  options?: QueryHookOptions<SearchUsersQueryData, SearchUsersQueryError>,
): UseQueryResult<SearchUsersQueryData, SearchUsersQueryError>;
/**
 * searchUsers telemetry
 *
 * @remarks
 * Search and list user usage summaries grouped by user_id or external_user_id
 */
export declare function useSearchUsersSuspense(
  request: SearchUsersRequest,
  security?: SearchUsersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    SearchUsersQueryData,
    SearchUsersQueryError
  >,
): UseSuspenseQueryResult<SearchUsersQueryData, SearchUsersQueryError>;
export declare function setSearchUsersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: SearchUsersQueryData,
): SearchUsersQueryData | undefined;
export declare function invalidateSearchUsers(
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
export declare function invalidateAllSearchUsers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=searchUsers.d.ts.map
