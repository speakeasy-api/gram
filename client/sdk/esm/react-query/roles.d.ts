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
  ListRolesRequest,
  ListRolesSecurity,
} from "../models/operations/listroles.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRolesQuery,
  prefetchRoles,
  queryKeyRoles,
  RolesQueryData,
} from "./roles.core.js";
export { buildRolesQuery, prefetchRoles, queryKeyRoles, type RolesQueryData };
export type RolesQueryError =
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
 * listRoles access
 *
 * @remarks
 * List all roles for the current organization.
 */
export declare function useRoles(
  request?: ListRolesRequest | undefined,
  security?: ListRolesSecurity | undefined,
  options?: QueryHookOptions<RolesQueryData, RolesQueryError>,
): UseQueryResult<RolesQueryData, RolesQueryError>;
/**
 * listRoles access
 *
 * @remarks
 * List all roles for the current organization.
 */
export declare function useRolesSuspense(
  request?: ListRolesRequest | undefined,
  security?: ListRolesSecurity | undefined,
  options?: SuspenseQueryHookOptions<RolesQueryData, RolesQueryError>,
): UseSuspenseQueryResult<RolesQueryData, RolesQueryError>;
export declare function setRolesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: RolesQueryData,
): RolesQueryData | undefined;
export declare function invalidateRoles(
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
export declare function invalidateAllRoles(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=roles.d.ts.map
