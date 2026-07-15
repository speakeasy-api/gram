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
  GetToolsetEnvironmentRequest,
  GetToolsetEnvironmentSecurity,
} from "../models/operations/gettoolsetenvironment.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetToolsetEnvironmentQuery,
  GetToolsetEnvironmentQueryData,
  prefetchGetToolsetEnvironment,
  queryKeyGetToolsetEnvironment,
} from "./getToolsetEnvironment.core.js";
export {
  buildGetToolsetEnvironmentQuery,
  type GetToolsetEnvironmentQueryData,
  prefetchGetToolsetEnvironment,
  queryKeyGetToolsetEnvironment,
};
export type GetToolsetEnvironmentQueryError =
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
 * getToolsetEnvironment environments
 *
 * @remarks
 * Get the environment linked to a toolset
 */
export declare function useGetToolsetEnvironment(
  request: GetToolsetEnvironmentRequest,
  security?: GetToolsetEnvironmentSecurity | undefined,
  options?: QueryHookOptions<
    GetToolsetEnvironmentQueryData,
    GetToolsetEnvironmentQueryError
  >,
): UseQueryResult<
  GetToolsetEnvironmentQueryData,
  GetToolsetEnvironmentQueryError
>;
/**
 * getToolsetEnvironment environments
 *
 * @remarks
 * Get the environment linked to a toolset
 */
export declare function useGetToolsetEnvironmentSuspense(
  request: GetToolsetEnvironmentRequest,
  security?: GetToolsetEnvironmentSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetToolsetEnvironmentQueryData,
    GetToolsetEnvironmentQueryError
  >,
): UseSuspenseQueryResult<
  GetToolsetEnvironmentQueryData,
  GetToolsetEnvironmentQueryError
>;
export declare function setGetToolsetEnvironmentData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      toolsetId: string;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetToolsetEnvironmentQueryData,
): GetToolsetEnvironmentQueryData | undefined;
export declare function invalidateGetToolsetEnvironment(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        toolsetId: string;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetToolsetEnvironment(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getToolsetEnvironment.d.ts.map
