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
  GetOrganizationRemoteSessionClientRequest,
  GetOrganizationRemoteSessionClientSecurity,
} from "../models/operations/getorganizationremotesessionclient.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOrganizationRemoteSessionClientQuery,
  OrganizationRemoteSessionClientQueryData,
  prefetchOrganizationRemoteSessionClient,
  queryKeyOrganizationRemoteSessionClient,
} from "./organizationRemoteSessionClient.core.js";
export {
  buildOrganizationRemoteSessionClientQuery,
  type OrganizationRemoteSessionClientQueryData,
  prefetchOrganizationRemoteSessionClient,
  queryKeyOrganizationRemoteSessionClient,
};
export type OrganizationRemoteSessionClientQueryError =
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
 * getClient organizationRemoteSessionClients
 *
 * @remarks
 * Get a remote_session_client in the caller's organization by id. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClient(
  request: GetOrganizationRemoteSessionClientRequest,
  security?: GetOrganizationRemoteSessionClientSecurity | undefined,
  options?: QueryHookOptions<
    OrganizationRemoteSessionClientQueryData,
    OrganizationRemoteSessionClientQueryError
  >,
): UseQueryResult<
  OrganizationRemoteSessionClientQueryData,
  OrganizationRemoteSessionClientQueryError
>;
/**
 * getClient organizationRemoteSessionClients
 *
 * @remarks
 * Get a remote_session_client in the caller's organization by id. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientSuspense(
  request: GetOrganizationRemoteSessionClientRequest,
  security?: GetOrganizationRemoteSessionClientSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OrganizationRemoteSessionClientQueryData,
    OrganizationRemoteSessionClientQueryError
  >,
): UseSuspenseQueryResult<
  OrganizationRemoteSessionClientQueryData,
  OrganizationRemoteSessionClientQueryError
>;
export declare function setOrganizationRemoteSessionClientData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
    },
  ],
  data: OrganizationRemoteSessionClientQueryData,
): OrganizationRemoteSessionClientQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionClient(
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
export declare function invalidateAllOrganizationRemoteSessionClient(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionClient.d.ts.map
