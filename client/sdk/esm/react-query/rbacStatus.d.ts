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
  GetRBACStatusRequest,
  GetRBACStatusSecurity,
} from "../models/operations/getrbacstatus.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRbacStatusQuery,
  prefetchRbacStatus,
  queryKeyRbacStatus,
  RbacStatusQueryData,
} from "./rbacStatus.core.js";
export {
  buildRbacStatusQuery,
  prefetchRbacStatus,
  queryKeyRbacStatus,
  type RbacStatusQueryData,
};
export type RbacStatusQueryError =
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
 * getRBACStatus access
 *
 * @remarks
 * Returns whether RBAC is currently enabled for the current organization.
 */
export declare function useRbacStatus(
  request?: GetRBACStatusRequest | undefined,
  security?: GetRBACStatusSecurity | undefined,
  options?: QueryHookOptions<RbacStatusQueryData, RbacStatusQueryError>,
): UseQueryResult<RbacStatusQueryData, RbacStatusQueryError>;
/**
 * getRBACStatus access
 *
 * @remarks
 * Returns whether RBAC is currently enabled for the current organization.
 */
export declare function useRbacStatusSuspense(
  request?: GetRBACStatusRequest | undefined,
  security?: GetRBACStatusSecurity | undefined,
  options?: SuspenseQueryHookOptions<RbacStatusQueryData, RbacStatusQueryError>,
): UseSuspenseQueryResult<RbacStatusQueryData, RbacStatusQueryError>;
export declare function setRbacStatusData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: RbacStatusQueryData,
): RbacStatusQueryData | undefined;
export declare function invalidateRbacStatus(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRbacStatus(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=rbacStatus.d.ts.map
