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
  ListOrganizationRemoteSessionClientMcpServersRequest,
  ListOrganizationRemoteSessionClientMcpServersSecurity,
} from "../models/operations/listorganizationremotesessionclientmcpservers.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOrganizationRemoteSessionClientMcpServersQuery,
  OrganizationRemoteSessionClientMcpServersQueryData,
  prefetchOrganizationRemoteSessionClientMcpServers,
  queryKeyOrganizationRemoteSessionClientMcpServers,
} from "./organizationRemoteSessionClientMcpServers.core.js";
export {
  buildOrganizationRemoteSessionClientMcpServersQuery,
  type OrganizationRemoteSessionClientMcpServersQueryData,
  prefetchOrganizationRemoteSessionClientMcpServers,
  queryKeyOrganizationRemoteSessionClientMcpServers,
};
export type OrganizationRemoteSessionClientMcpServersQueryError =
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
 * listClientMcpServers organizationRemoteSessionClients
 *
 * @remarks
 * List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientMcpServers(
  request: ListOrganizationRemoteSessionClientMcpServersRequest,
  security?: ListOrganizationRemoteSessionClientMcpServersSecurity | undefined,
  options?: QueryHookOptions<
    OrganizationRemoteSessionClientMcpServersQueryData,
    OrganizationRemoteSessionClientMcpServersQueryError
  >,
): UseQueryResult<
  OrganizationRemoteSessionClientMcpServersQueryData,
  OrganizationRemoteSessionClientMcpServersQueryError
>;
/**
 * listClientMcpServers organizationRemoteSessionClients
 *
 * @remarks
 * List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.
 */
export declare function useOrganizationRemoteSessionClientMcpServersSuspense(
  request: ListOrganizationRemoteSessionClientMcpServersRequest,
  security?: ListOrganizationRemoteSessionClientMcpServersSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OrganizationRemoteSessionClientMcpServersQueryData,
    OrganizationRemoteSessionClientMcpServersQueryError
  >,
): UseSuspenseQueryResult<
  OrganizationRemoteSessionClientMcpServersQueryData,
  OrganizationRemoteSessionClientMcpServersQueryError
>;
export declare function setOrganizationRemoteSessionClientMcpServersData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      clientId: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
    },
  ],
  data: OrganizationRemoteSessionClientMcpServersQueryData,
): OrganizationRemoteSessionClientMcpServersQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionClientMcpServers(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        clientId: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllOrganizationRemoteSessionClientMcpServers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionClientMcpServers.d.ts.map
