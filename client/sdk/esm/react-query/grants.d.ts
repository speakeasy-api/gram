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
  ListGrantsRequest,
  ListGrantsSecurity,
} from "../models/operations/listgrants.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGrantsQuery,
  GrantsQueryData,
  prefetchGrants,
  queryKeyGrants,
} from "./grants.core.js";
export {
  buildGrantsQuery,
  type GrantsQueryData,
  prefetchGrants,
  queryKeyGrants,
};
export type GrantsQueryError =
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
 * listGrants access
 *
 * @remarks
 * List the current user's effective grants, including inherited role grants.
 */
export declare function useGrants(
  request?: ListGrantsRequest | undefined,
  security?: ListGrantsSecurity | undefined,
  options?: QueryHookOptions<GrantsQueryData, GrantsQueryError>,
): UseQueryResult<GrantsQueryData, GrantsQueryError>;
/**
 * listGrants access
 *
 * @remarks
 * List the current user's effective grants, including inherited role grants.
 */
export declare function useGrantsSuspense(
  request?: ListGrantsRequest | undefined,
  security?: ListGrantsSecurity | undefined,
  options?: SuspenseQueryHookOptions<GrantsQueryData, GrantsQueryError>,
): UseSuspenseQueryResult<GrantsQueryData, GrantsQueryError>;
export declare function setGrantsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: GrantsQueryData,
): GrantsQueryData | undefined;
export declare function invalidateGrants(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGrants(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=grants.d.ts.map
