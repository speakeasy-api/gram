import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ServeChatAttachmentRequest, ServeChatAttachmentSecurity } from "../models/operations/servechatattachment.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildServeChatAttachmentQuery, prefetchServeChatAttachment, queryKeyServeChatAttachment, ServeChatAttachmentQueryData } from "./serveChatAttachment.core.js";
export { buildServeChatAttachmentQuery, prefetchServeChatAttachment, queryKeyServeChatAttachment, type ServeChatAttachmentQueryData, };
export type ServeChatAttachmentQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * serveChatAttachment assets
 *
 * @remarks
 * Serve a chat attachment from Gram.
 */
export declare function useServeChatAttachment(request: ServeChatAttachmentRequest, security?: ServeChatAttachmentSecurity | undefined, options?: QueryHookOptions<ServeChatAttachmentQueryData, ServeChatAttachmentQueryError>): UseQueryResult<ServeChatAttachmentQueryData, ServeChatAttachmentQueryError>;
/**
 * serveChatAttachment assets
 *
 * @remarks
 * Serve a chat attachment from Gram.
 */
export declare function useServeChatAttachmentSuspense(request: ServeChatAttachmentRequest, security?: ServeChatAttachmentSecurity | undefined, options?: SuspenseQueryHookOptions<ServeChatAttachmentQueryData, ServeChatAttachmentQueryError>): UseSuspenseQueryResult<ServeChatAttachmentQueryData, ServeChatAttachmentQueryError>;
export declare function setServeChatAttachmentData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        projectId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramChatSession?: string | undefined;
    }
], data: ServeChatAttachmentQueryData): ServeChatAttachmentQueryData | undefined;
export declare function invalidateServeChatAttachment(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        projectId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramChatSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllServeChatAttachment(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=serveChatAttachment.d.ts.map