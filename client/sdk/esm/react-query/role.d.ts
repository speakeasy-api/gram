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
  GetRoleRequest,
  GetRoleSecurity,
} from "../models/operations/getrole.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRoleQuery,
  prefetchRole,
  queryKeyRole,
  RoleQueryData,
} from "./role.core.js";
export { buildRoleQuery, prefetchRole, queryKeyRole, type RoleQueryData };
export type RoleQueryError =
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
 * getRole access
 *
 * @remarks
 * Get a role by ID.
 */
export declare function useRole(
  request: GetRoleRequest,
  security?: GetRoleSecurity | undefined,
  options?: QueryHookOptions<RoleQueryData, RoleQueryError>,
): UseQueryResult<RoleQueryData, RoleQueryError>;
/**
 * getRole access
 *
 * @remarks
 * Get a role by ID.
 */
export declare function useRoleSuspense(
  request: GetRoleRequest,
  security?: GetRoleSecurity | undefined,
  options?: SuspenseQueryHookOptions<RoleQueryData, RoleQueryError>,
): UseSuspenseQueryResult<RoleQueryData, RoleQueryError>;
export declare function setRoleData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: RoleQueryData,
): RoleQueryData | undefined;
export declare function invalidateRole(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRole(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=role.d.ts.map
