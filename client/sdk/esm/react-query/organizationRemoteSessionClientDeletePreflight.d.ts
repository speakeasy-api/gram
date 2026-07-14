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
  GetOrganizationRemoteSessionClientDeletePreflightRequest,
  GetOrganizationRemoteSessionClientDeletePreflightSecurity,
} from "../models/operations/getorganizationremotesessionclientdeletepreflight.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOrganizationRemoteSessionClientDeletePreflightQuery,
  OrganizationRemoteSessionClientDeletePreflightQueryData,
  prefetchOrganizationRemoteSessionClientDeletePreflight,
  queryKeyOrganizationRemoteSessionClientDeletePreflight,
} from "./organizationRemoteSessionClientDeletePreflight.core.js";
export {
  buildOrganizationRemoteSessionClientDeletePreflightQuery,
  type OrganizationRemoteSessionClientDeletePreflightQueryData,
  prefetchOrganizationRemoteSessionClientDeletePreflight,
  queryKeyOrganizationRemoteSessionClientDeletePreflight,
};
export type OrganizationRemoteSessionClientDeletePreflightQueryError =
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
 * getClientDeletePreflight organizationRemoteSessionClients
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_client: associated session count and affected MCP server names. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientDeletePreflight(
  request: GetOrganizationRemoteSessionClientDeletePreflightRequest,
  security?:
    | GetOrganizationRemoteSessionClientDeletePreflightSecurity
    | undefined,
  options?: QueryHookOptions<
    OrganizationRemoteSessionClientDeletePreflightQueryData,
    OrganizationRemoteSessionClientDeletePreflightQueryError
  >,
): UseQueryResult<
  OrganizationRemoteSessionClientDeletePreflightQueryData,
  OrganizationRemoteSessionClientDeletePreflightQueryError
>;
/**
 * getClientDeletePreflight organizationRemoteSessionClients
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_client: associated session count and affected MCP server names. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientDeletePreflightSuspense(
  request: GetOrganizationRemoteSessionClientDeletePreflightRequest,
  security?:
    | GetOrganizationRemoteSessionClientDeletePreflightSecurity
    | undefined,
  options?: SuspenseQueryHookOptions<
    OrganizationRemoteSessionClientDeletePreflightQueryData,
    OrganizationRemoteSessionClientDeletePreflightQueryError
  >,
): UseSuspenseQueryResult<
  OrganizationRemoteSessionClientDeletePreflightQueryData,
  OrganizationRemoteSessionClientDeletePreflightQueryError
>;
export declare function setOrganizationRemoteSessionClientDeletePreflightData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
    },
  ],
  data: OrganizationRemoteSessionClientDeletePreflightQueryData,
): OrganizationRemoteSessionClientDeletePreflightQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionClientDeletePreflight(
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
export declare function invalidateAllOrganizationRemoteSessionClientDeletePreflight(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionClientDeletePreflight.d.ts.map
