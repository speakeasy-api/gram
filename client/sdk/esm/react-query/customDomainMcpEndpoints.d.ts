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
  ListCustomDomainMcpEndpointsRequest,
  ListCustomDomainMcpEndpointsSecurity,
} from "../models/operations/listcustomdomainmcpendpoints.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildCustomDomainMcpEndpointsQuery,
  CustomDomainMcpEndpointsQueryData,
  prefetchCustomDomainMcpEndpoints,
  queryKeyCustomDomainMcpEndpoints,
} from "./customDomainMcpEndpoints.core.js";
export {
  buildCustomDomainMcpEndpointsQuery,
  type CustomDomainMcpEndpointsQueryData,
  prefetchCustomDomainMcpEndpoints,
  queryKeyCustomDomainMcpEndpoints,
};
export type CustomDomainMcpEndpointsQueryError =
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
 * listMcpEndpoints domains
 *
 * @remarks
 * List the MCP endpoints registered under the organization's custom domain across every project. Returns enriched rows that include the parent MCP server and project so callers can preview what a custom-domain deletion would cascade through.
 */
export declare function useCustomDomainMcpEndpoints(
  request?: ListCustomDomainMcpEndpointsRequest | undefined,
  security?: ListCustomDomainMcpEndpointsSecurity | undefined,
  options?: QueryHookOptions<
    CustomDomainMcpEndpointsQueryData,
    CustomDomainMcpEndpointsQueryError
  >,
): UseQueryResult<
  CustomDomainMcpEndpointsQueryData,
  CustomDomainMcpEndpointsQueryError
>;
/**
 * listMcpEndpoints domains
 *
 * @remarks
 * List the MCP endpoints registered under the organization's custom domain across every project. Returns enriched rows that include the parent MCP server and project so callers can preview what a custom-domain deletion would cascade through.
 */
export declare function useCustomDomainMcpEndpointsSuspense(
  request?: ListCustomDomainMcpEndpointsRequest | undefined,
  security?: ListCustomDomainMcpEndpointsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    CustomDomainMcpEndpointsQueryData,
    CustomDomainMcpEndpointsQueryError
  >,
): UseSuspenseQueryResult<
  CustomDomainMcpEndpointsQueryData,
  CustomDomainMcpEndpointsQueryError
>;
export declare function setCustomDomainMcpEndpointsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: CustomDomainMcpEndpointsQueryData,
): CustomDomainMcpEndpointsQueryData | undefined;
export declare function invalidateCustomDomainMcpEndpoints(
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
export declare function invalidateAllCustomDomainMcpEndpoints(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=customDomainMcpEndpoints.d.ts.map
