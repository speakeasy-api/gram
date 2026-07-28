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
  GetPublishStatusRequest,
  GetPublishStatusSecurity,
} from "../models/operations/getpublishstatus.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildPublishStatusQuery,
  prefetchPublishStatus,
  PublishStatusQueryData,
  queryKeyPublishStatus,
} from "./publishStatus.core.js";
export {
  buildPublishStatusQuery,
  prefetchPublishStatus,
  type PublishStatusQueryData,
  queryKeyPublishStatus,
};
export type PublishStatusQueryError =
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
 * getPublishStatus plugins
 *
 * @remarks
 * Check whether GitHub publishing is configured and connected for this project.
 */
export declare function usePublishStatus(
  request?: GetPublishStatusRequest | undefined,
  security?: GetPublishStatusSecurity | undefined,
  options?: QueryHookOptions<PublishStatusQueryData, PublishStatusQueryError>,
): UseQueryResult<PublishStatusQueryData, PublishStatusQueryError>;
/**
 * getPublishStatus plugins
 *
 * @remarks
 * Check whether GitHub publishing is configured and connected for this project.
 */
export declare function usePublishStatusSuspense(
  request?: GetPublishStatusRequest | undefined,
  security?: GetPublishStatusSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    PublishStatusQueryData,
    PublishStatusQueryError
  >,
): UseSuspenseQueryResult<PublishStatusQueryData, PublishStatusQueryError>;
export declare function setPublishStatusData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: PublishStatusQueryData,
): PublishStatusQueryData | undefined;
export declare function invalidatePublishStatus(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllPublishStatus(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=publishStatus.d.ts.map
