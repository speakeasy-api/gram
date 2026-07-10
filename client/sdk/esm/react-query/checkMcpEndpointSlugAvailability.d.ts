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
  CheckMcpEndpointSlugAvailabilityRequest,
  CheckMcpEndpointSlugAvailabilitySecurity,
} from "../models/operations/checkmcpendpointslugavailability.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildCheckMcpEndpointSlugAvailabilityQuery,
  CheckMcpEndpointSlugAvailabilityQueryData,
  prefetchCheckMcpEndpointSlugAvailability,
  queryKeyCheckMcpEndpointSlugAvailability,
} from "./checkMcpEndpointSlugAvailability.core.js";
export {
  buildCheckMcpEndpointSlugAvailabilityQuery,
  type CheckMcpEndpointSlugAvailabilityQueryData,
  prefetchCheckMcpEndpointSlugAvailability,
  queryKeyCheckMcpEndpointSlugAvailability,
};
export type CheckMcpEndpointSlugAvailabilityQueryError =
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
 * checkMcpEndpointSlugAvailability mcpEndpoints
 *
 * @remarks
 * Check whether an MCP endpoint slug is available. The uniqueness scope depends on whether a custom_domain_id is provided: platform-domain slugs are checked across all platform-domain endpoints (custom_domain_id IS NULL); custom-domain slugs are checked within the (custom_domain_id, slug) pair. Returns true when the slug is free.
 */
export declare function useCheckMcpEndpointSlugAvailability(
  request: CheckMcpEndpointSlugAvailabilityRequest,
  security?: CheckMcpEndpointSlugAvailabilitySecurity | undefined,
  options?: QueryHookOptions<
    CheckMcpEndpointSlugAvailabilityQueryData,
    CheckMcpEndpointSlugAvailabilityQueryError
  >,
): UseQueryResult<
  CheckMcpEndpointSlugAvailabilityQueryData,
  CheckMcpEndpointSlugAvailabilityQueryError
>;
/**
 * checkMcpEndpointSlugAvailability mcpEndpoints
 *
 * @remarks
 * Check whether an MCP endpoint slug is available. The uniqueness scope depends on whether a custom_domain_id is provided: platform-domain slugs are checked across all platform-domain endpoints (custom_domain_id IS NULL); custom-domain slugs are checked within the (custom_domain_id, slug) pair. Returns true when the slug is free.
 */
export declare function useCheckMcpEndpointSlugAvailabilitySuspense(
  request: CheckMcpEndpointSlugAvailabilityRequest,
  security?: CheckMcpEndpointSlugAvailabilitySecurity | undefined,
  options?: SuspenseQueryHookOptions<
    CheckMcpEndpointSlugAvailabilityQueryData,
    CheckMcpEndpointSlugAvailabilityQueryError
  >,
): UseSuspenseQueryResult<
  CheckMcpEndpointSlugAvailabilityQueryData,
  CheckMcpEndpointSlugAvailabilityQueryError
>;
export declare function setCheckMcpEndpointSlugAvailabilityData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      slug: string;
      customDomainId?: string | undefined;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: CheckMcpEndpointSlugAvailabilityQueryData,
): CheckMcpEndpointSlugAvailabilityQueryData | undefined;
export declare function invalidateCheckMcpEndpointSlugAvailability(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        slug: string;
        customDomainId?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllCheckMcpEndpointSlugAvailability(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=checkMcpEndpointSlugAvailability.d.ts.map
