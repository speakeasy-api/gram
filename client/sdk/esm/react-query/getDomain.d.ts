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
  GetDomainRequest,
  GetDomainSecurity,
} from "../models/operations/getdomain.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetDomainQuery,
  GetDomainQueryData,
  prefetchGetDomain,
  queryKeyGetDomain,
} from "./getDomain.core.js";
export {
  buildGetDomainQuery,
  type GetDomainQueryData,
  prefetchGetDomain,
  queryKeyGetDomain,
};
export type GetDomainQueryError =
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
 * getDomain domains
 *
 * @remarks
 * Get the custom domain for an organization
 */
export declare function useGetDomain(
  request?: GetDomainRequest | undefined,
  security?: GetDomainSecurity | undefined,
  options?: QueryHookOptions<GetDomainQueryData, GetDomainQueryError>,
): UseQueryResult<GetDomainQueryData, GetDomainQueryError>;
/**
 * getDomain domains
 *
 * @remarks
 * Get the custom domain for an organization
 */
export declare function useGetDomainSuspense(
  request?: GetDomainRequest | undefined,
  security?: GetDomainSecurity | undefined,
  options?: SuspenseQueryHookOptions<GetDomainQueryData, GetDomainQueryError>,
): UseSuspenseQueryResult<GetDomainQueryData, GetDomainQueryError>;
export declare function setGetDomainData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: GetDomainQueryData,
): GetDomainQueryData | undefined;
export declare function invalidateGetDomain(
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
export declare function invalidateAllGetDomain(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getDomain.d.ts.map
