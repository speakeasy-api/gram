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
import { ServeImageRequest } from "../models/operations/serveimage.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildServeImageQuery,
  prefetchServeImage,
  queryKeyServeImage,
  ServeImageQueryData,
} from "./serveImage.core.js";
export {
  buildServeImageQuery,
  prefetchServeImage,
  queryKeyServeImage,
  type ServeImageQueryData,
};
export type ServeImageQueryError =
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
 * serveImage assets
 *
 * @remarks
 * Serve an image from Gram.
 */
export declare function useServeImage(
  request: ServeImageRequest,
  options?: QueryHookOptions<ServeImageQueryData, ServeImageQueryError>,
): UseQueryResult<ServeImageQueryData, ServeImageQueryError>;
/**
 * serveImage assets
 *
 * @remarks
 * Serve an image from Gram.
 */
export declare function useServeImageSuspense(
  request: ServeImageRequest,
  options?: SuspenseQueryHookOptions<ServeImageQueryData, ServeImageQueryError>,
): UseSuspenseQueryResult<ServeImageQueryData, ServeImageQueryError>;
export declare function setServeImageData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
    },
  ],
  data: ServeImageQueryData,
): ServeImageQueryData | undefined;
export declare function invalidateServeImage(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllServeImage(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=serveImage.d.ts.map
