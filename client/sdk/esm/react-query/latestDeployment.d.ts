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
  GetLatestDeploymentRequest,
  GetLatestDeploymentSecurity,
} from "../models/operations/getlatestdeployment.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildLatestDeploymentQuery,
  LatestDeploymentQueryData,
  prefetchLatestDeployment,
  queryKeyLatestDeployment,
} from "./latestDeployment.core.js";
export {
  buildLatestDeploymentQuery,
  type LatestDeploymentQueryData,
  prefetchLatestDeployment,
  queryKeyLatestDeployment,
};
export type LatestDeploymentQueryError =
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
 * getLatestDeployment deployments
 *
 * @remarks
 * Get the latest deployment for a project.
 */
export declare function useLatestDeployment(
  request?: GetLatestDeploymentRequest | undefined,
  security?: GetLatestDeploymentSecurity | undefined,
  options?: QueryHookOptions<
    LatestDeploymentQueryData,
    LatestDeploymentQueryError
  >,
): UseQueryResult<LatestDeploymentQueryData, LatestDeploymentQueryError>;
/**
 * getLatestDeployment deployments
 *
 * @remarks
 * Get the latest deployment for a project.
 */
export declare function useLatestDeploymentSuspense(
  request?: GetLatestDeploymentRequest | undefined,
  security?: GetLatestDeploymentSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    LatestDeploymentQueryData,
    LatestDeploymentQueryError
  >,
): UseSuspenseQueryResult<
  LatestDeploymentQueryData,
  LatestDeploymentQueryError
>;
export declare function setLatestDeploymentData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: LatestDeploymentQueryData,
): LatestDeploymentQueryData | undefined;
export declare function invalidateLatestDeployment(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllLatestDeployment(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=latestDeployment.d.ts.map
