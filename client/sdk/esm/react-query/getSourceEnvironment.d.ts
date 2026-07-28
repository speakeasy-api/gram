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
  GetSourceEnvironmentRequest,
  GetSourceEnvironmentSecurity,
  QueryParamSourceKind,
} from "../models/operations/getsourceenvironment.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetSourceEnvironmentQuery,
  GetSourceEnvironmentQueryData,
  prefetchGetSourceEnvironment,
  queryKeyGetSourceEnvironment,
} from "./getSourceEnvironment.core.js";
export {
  buildGetSourceEnvironmentQuery,
  type GetSourceEnvironmentQueryData,
  prefetchGetSourceEnvironment,
  queryKeyGetSourceEnvironment,
};
export type GetSourceEnvironmentQueryError =
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
 * getSourceEnvironment environments
 *
 * @remarks
 * Get the environment linked to a source
 */
export declare function useGetSourceEnvironment(
  request: GetSourceEnvironmentRequest,
  security?: GetSourceEnvironmentSecurity | undefined,
  options?: QueryHookOptions<
    GetSourceEnvironmentQueryData,
    GetSourceEnvironmentQueryError
  >,
): UseQueryResult<
  GetSourceEnvironmentQueryData,
  GetSourceEnvironmentQueryError
>;
/**
 * getSourceEnvironment environments
 *
 * @remarks
 * Get the environment linked to a source
 */
export declare function useGetSourceEnvironmentSuspense(
  request: GetSourceEnvironmentRequest,
  security?: GetSourceEnvironmentSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetSourceEnvironmentQueryData,
    GetSourceEnvironmentQueryError
  >,
): UseSuspenseQueryResult<
  GetSourceEnvironmentQueryData,
  GetSourceEnvironmentQueryError
>;
export declare function setGetSourceEnvironmentData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      sourceKind: QueryParamSourceKind;
      sourceSlug: string;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetSourceEnvironmentQueryData,
): GetSourceEnvironmentQueryData | undefined;
export declare function invalidateGetSourceEnvironment(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        sourceKind: QueryParamSourceKind;
        sourceSlug: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetSourceEnvironment(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getSourceEnvironment.d.ts.map
