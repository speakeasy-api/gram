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
  GetOrganizationRequest,
  GetOrganizationSecurity,
} from "../models/operations/getorganization.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOrganizationQuery,
  OrganizationQueryData,
  prefetchOrganization,
  queryKeyOrganization,
} from "./organization.core.js";
export {
  buildOrganizationQuery,
  type OrganizationQueryData,
  prefetchOrganization,
  queryKeyOrganization,
};
export type OrganizationQueryError =
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
 * get organizations
 *
 * @remarks
 * Get the active organization from the session.
 */
export declare function useOrganization(
  request?: GetOrganizationRequest | undefined,
  security?: GetOrganizationSecurity | undefined,
  options?: QueryHookOptions<OrganizationQueryData, OrganizationQueryError>,
): UseQueryResult<OrganizationQueryData, OrganizationQueryError>;
/**
 * get organizations
 *
 * @remarks
 * Get the active organization from the session.
 */
export declare function useOrganizationSuspense(
  request?: GetOrganizationRequest | undefined,
  security?: GetOrganizationSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OrganizationQueryData,
    OrganizationQueryError
  >,
): UseSuspenseQueryResult<OrganizationQueryData, OrganizationQueryError>;
export declare function setOrganizationData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: OrganizationQueryData,
): OrganizationQueryData | undefined;
export declare function invalidateOrganization(
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
export declare function invalidateAllOrganization(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=organization.d.ts.map
