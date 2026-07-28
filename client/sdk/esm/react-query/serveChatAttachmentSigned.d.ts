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
import { ServeChatAttachmentSignedRequest } from "../models/operations/servechatattachmentsigned.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildServeChatAttachmentSignedQuery,
  prefetchServeChatAttachmentSigned,
  queryKeyServeChatAttachmentSigned,
  ServeChatAttachmentSignedQueryData,
} from "./serveChatAttachmentSigned.core.js";
export {
  buildServeChatAttachmentSignedQuery,
  prefetchServeChatAttachmentSigned,
  queryKeyServeChatAttachmentSigned,
  type ServeChatAttachmentSignedQueryData,
};
export type ServeChatAttachmentSignedQueryError =
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
 * serveChatAttachmentSigned assets
 *
 * @remarks
 * Serve a chat attachment using a signed URL token.
 */
export declare function useServeChatAttachmentSigned(
  request: ServeChatAttachmentSignedRequest,
  options?: QueryHookOptions<
    ServeChatAttachmentSignedQueryData,
    ServeChatAttachmentSignedQueryError
  >,
): UseQueryResult<
  ServeChatAttachmentSignedQueryData,
  ServeChatAttachmentSignedQueryError
>;
/**
 * serveChatAttachmentSigned assets
 *
 * @remarks
 * Serve a chat attachment using a signed URL token.
 */
export declare function useServeChatAttachmentSignedSuspense(
  request: ServeChatAttachmentSignedRequest,
  options?: SuspenseQueryHookOptions<
    ServeChatAttachmentSignedQueryData,
    ServeChatAttachmentSignedQueryError
  >,
): UseSuspenseQueryResult<
  ServeChatAttachmentSignedQueryData,
  ServeChatAttachmentSignedQueryError
>;
export declare function setServeChatAttachmentSignedData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      token: string;
    },
  ],
  data: ServeChatAttachmentSignedQueryData,
): ServeChatAttachmentSignedQueryData | undefined;
export declare function invalidateServeChatAttachmentSigned(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        token: string;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllServeChatAttachmentSigned(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=serveChatAttachmentSigned.d.ts.map
