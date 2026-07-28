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
  GetOrganizationRemoteSessionIssuerRequest,
  GetOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/getorganizationremotesessionissuer.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOrganizationRemoteSessionIssuerQuery,
  OrganizationRemoteSessionIssuerQueryData,
  prefetchOrganizationRemoteSessionIssuer,
  queryKeyOrganizationRemoteSessionIssuer,
} from "./organizationRemoteSessionIssuer.core.js";
export {
  buildOrganizationRemoteSessionIssuerQuery,
  type OrganizationRemoteSessionIssuerQueryData,
  prefetchOrganizationRemoteSessionIssuer,
  queryKeyOrganizationRemoteSessionIssuer,
};
export type OrganizationRemoteSessionIssuerQueryError =
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
 * getIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Get any remote_session_issuer (organizational or project-specific) in the caller's organization by id. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuer(
  request: GetOrganizationRemoteSessionIssuerRequest,
  security?: GetOrganizationRemoteSessionIssuerSecurity | undefined,
  options?: QueryHookOptions<
    OrganizationRemoteSessionIssuerQueryData,
    OrganizationRemoteSessionIssuerQueryError
  >,
): UseQueryResult<
  OrganizationRemoteSessionIssuerQueryData,
  OrganizationRemoteSessionIssuerQueryError
>;
/**
 * getIssuer organizationRemoteSessionIssuers
 *
 * @remarks
 * Get any remote_session_issuer (organizational or project-specific) in the caller's organization by id. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuerSuspense(
  request: GetOrganizationRemoteSessionIssuerRequest,
  security?: GetOrganizationRemoteSessionIssuerSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OrganizationRemoteSessionIssuerQueryData,
    OrganizationRemoteSessionIssuerQueryError
  >,
): UseSuspenseQueryResult<
  OrganizationRemoteSessionIssuerQueryData,
  OrganizationRemoteSessionIssuerQueryError
>;
export declare function setOrganizationRemoteSessionIssuerData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
    },
  ],
  data: OrganizationRemoteSessionIssuerQueryData,
): OrganizationRemoteSessionIssuerQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionIssuer(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllOrganizationRemoteSessionIssuer(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionIssuer.d.ts.map
