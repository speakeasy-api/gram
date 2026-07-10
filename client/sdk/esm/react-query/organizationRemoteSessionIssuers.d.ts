import {
  InfiniteData,
  InvalidateQueryFilters,
  QueryClient,
  UseInfiniteQueryResult,
  UseQueryResult,
  UseSuspenseInfiniteQueryResult,
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
  ListOrganizationRemoteSessionIssuersRequest,
  ListOrganizationRemoteSessionIssuersSecurity,
} from "../models/operations/listorganizationremotesessionissuers.js";
import {
  InfiniteQueryHookOptions,
  QueryHookOptions,
  SuspenseInfiniteQueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOrganizationRemoteSessionIssuersInfiniteQuery,
  buildOrganizationRemoteSessionIssuersQuery,
  OrganizationRemoteSessionIssuersInfiniteQueryData,
  OrganizationRemoteSessionIssuersPageParams,
  OrganizationRemoteSessionIssuersQueryData,
  prefetchOrganizationRemoteSessionIssuers,
  prefetchOrganizationRemoteSessionIssuersInfinite,
  queryKeyOrganizationRemoteSessionIssuers,
  queryKeyOrganizationRemoteSessionIssuersInfinite,
} from "./organizationRemoteSessionIssuers.core.js";
export {
  buildOrganizationRemoteSessionIssuersInfiniteQuery,
  buildOrganizationRemoteSessionIssuersQuery,
  type OrganizationRemoteSessionIssuersInfiniteQueryData,
  type OrganizationRemoteSessionIssuersPageParams,
  type OrganizationRemoteSessionIssuersQueryData,
  prefetchOrganizationRemoteSessionIssuers,
  prefetchOrganizationRemoteSessionIssuersInfinite,
  queryKeyOrganizationRemoteSessionIssuers,
  queryKeyOrganizationRemoteSessionIssuersInfinite,
};
export type OrganizationRemoteSessionIssuersQueryError =
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
 * listIssuers organizationRemoteSessionIssuers
 *
 * @remarks
 * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuers(
  request?: ListOrganizationRemoteSessionIssuersRequest | undefined,
  security?: ListOrganizationRemoteSessionIssuersSecurity | undefined,
  options?: QueryHookOptions<
    OrganizationRemoteSessionIssuersQueryData,
    OrganizationRemoteSessionIssuersQueryError
  >,
): UseQueryResult<
  OrganizationRemoteSessionIssuersQueryData,
  OrganizationRemoteSessionIssuersQueryError
>;
/**
 * listIssuers organizationRemoteSessionIssuers
 *
 * @remarks
 * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuersSuspense(
  request?: ListOrganizationRemoteSessionIssuersRequest | undefined,
  security?: ListOrganizationRemoteSessionIssuersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OrganizationRemoteSessionIssuersQueryData,
    OrganizationRemoteSessionIssuersQueryError
  >,
): UseSuspenseQueryResult<
  OrganizationRemoteSessionIssuersQueryData,
  OrganizationRemoteSessionIssuersQueryError
>;
/**
 * listIssuers organizationRemoteSessionIssuers
 *
 * @remarks
 * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuersInfinite(
  request?: ListOrganizationRemoteSessionIssuersRequest | undefined,
  security?: ListOrganizationRemoteSessionIssuersSecurity | undefined,
  options?: InfiniteQueryHookOptions<
    OrganizationRemoteSessionIssuersInfiniteQueryData,
    OrganizationRemoteSessionIssuersQueryError
  >,
): UseInfiniteQueryResult<
  InfiniteData<
    OrganizationRemoteSessionIssuersInfiniteQueryData,
    OrganizationRemoteSessionIssuersPageParams
  >,
  OrganizationRemoteSessionIssuersQueryError
>;
/**
 * listIssuers organizationRemoteSessionIssuers
 *
 * @remarks
 * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuersInfiniteSuspense(
  request?: ListOrganizationRemoteSessionIssuersRequest | undefined,
  security?: ListOrganizationRemoteSessionIssuersSecurity | undefined,
  options?: SuspenseInfiniteQueryHookOptions<
    OrganizationRemoteSessionIssuersInfiniteQueryData,
    OrganizationRemoteSessionIssuersQueryError
  >,
): UseSuspenseInfiniteQueryResult<
  InfiniteData<
    OrganizationRemoteSessionIssuersInfiniteQueryData,
    OrganizationRemoteSessionIssuersPageParams
  >,
  OrganizationRemoteSessionIssuersQueryError
>;
export declare function setOrganizationRemoteSessionIssuersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      cursor?: string | undefined;
      limit?: number | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
    },
  ],
  data: OrganizationRemoteSessionIssuersQueryData,
): OrganizationRemoteSessionIssuersQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionIssuers(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllOrganizationRemoteSessionIssuers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionIssuers.d.ts.map
