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
  CheckMCPSlugAvailabilityRequest,
  CheckMCPSlugAvailabilitySecurity,
} from "../models/operations/checkmcpslugavailability.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildCheckMCPSlugAvailabilityQuery,
  CheckMCPSlugAvailabilityQueryData,
  prefetchCheckMCPSlugAvailability,
  queryKeyCheckMCPSlugAvailability,
} from "./checkMCPSlugAvailability.core.js";
export {
  buildCheckMCPSlugAvailabilityQuery,
  type CheckMCPSlugAvailabilityQueryData,
  prefetchCheckMCPSlugAvailability,
  queryKeyCheckMCPSlugAvailability,
};
export type CheckMCPSlugAvailabilityQueryError =
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
 * checkMCPSlugAvailability toolsets
 *
 * @remarks
 * Check if a MCP slug is available
 */
export declare function useCheckMCPSlugAvailability(
  request: CheckMCPSlugAvailabilityRequest,
  security?: CheckMCPSlugAvailabilitySecurity | undefined,
  options?: QueryHookOptions<
    CheckMCPSlugAvailabilityQueryData,
    CheckMCPSlugAvailabilityQueryError
  >,
): UseQueryResult<
  CheckMCPSlugAvailabilityQueryData,
  CheckMCPSlugAvailabilityQueryError
>;
/**
 * checkMCPSlugAvailability toolsets
 *
 * @remarks
 * Check if a MCP slug is available
 */
export declare function useCheckMCPSlugAvailabilitySuspense(
  request: CheckMCPSlugAvailabilityRequest,
  security?: CheckMCPSlugAvailabilitySecurity | undefined,
  options?: SuspenseQueryHookOptions<
    CheckMCPSlugAvailabilityQueryData,
    CheckMCPSlugAvailabilityQueryError
  >,
): UseSuspenseQueryResult<
  CheckMCPSlugAvailabilityQueryData,
  CheckMCPSlugAvailabilityQueryError
>;
export declare function setCheckMCPSlugAvailabilityData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      slug: string;
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: CheckMCPSlugAvailabilityQueryData,
): CheckMCPSlugAvailabilityQueryData | undefined;
export declare function invalidateCheckMCPSlugAvailability(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        slug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllCheckMCPSlugAvailability(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=checkMCPSlugAvailability.d.ts.map
