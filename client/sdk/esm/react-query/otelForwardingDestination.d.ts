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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOtelForwardingDestinationQuery,
  OtelForwardingDestinationQueryData,
  prefetchOtelForwardingDestination,
  queryKeyOtelForwardingDestination,
} from "./otelForwardingDestination.core.js";
export {
  buildOtelForwardingDestinationQuery,
  type OtelForwardingDestinationQueryData,
  prefetchOtelForwardingDestination,
  queryKeyOtelForwardingDestination,
};
export type OtelForwardingDestinationQueryError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * getDestination otelForwarding
 *
 * @remarks
 * Get a single OTEL forwarding destination by ID.
 */
export declare function useOtelForwardingDestination(
  request: operations.GetOtelForwardingDestinationRequest,
  security?: operations.GetOtelForwardingDestinationSecurity | undefined,
  options?: QueryHookOptions<
    OtelForwardingDestinationQueryData,
    OtelForwardingDestinationQueryError
  >,
): UseQueryResult<
  OtelForwardingDestinationQueryData,
  OtelForwardingDestinationQueryError
>;
/**
 * getDestination otelForwarding
 *
 * @remarks
 * Get a single OTEL forwarding destination by ID.
 */
export declare function useOtelForwardingDestinationSuspense(
  request: operations.GetOtelForwardingDestinationRequest,
  security?: operations.GetOtelForwardingDestinationSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OtelForwardingDestinationQueryData,
    OtelForwardingDestinationQueryError
  >,
): UseSuspenseQueryResult<
  OtelForwardingDestinationQueryData,
  OtelForwardingDestinationQueryError
>;
export declare function setOtelForwardingDestinationData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: OtelForwardingDestinationQueryData,
): OtelForwardingDestinationQueryData | undefined;
export declare function invalidateOtelForwardingDestination(
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
export declare function invalidateAllOtelForwardingDestination(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=otelForwardingDestination.d.ts.map
