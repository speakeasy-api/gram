import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ServeChatAttachmentSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
    chatSessionsTokenHeaderGramChatSession?: string | undefined;
};
export type ServeChatAttachmentRequest = {
    /**
     * The ID of the attachment to serve
     */
    id: string;
    /**
     * The project ID that the attachment belongs to
     */
    projectId: string;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * Chat Sessions token header
     */
    gramChatSession?: string | undefined;
};
export type ServeChatAttachmentResponse = {
    headers: {
        [k: string]: Array<string>;
    };
    result: ReadableStream<Uint8Array>;
};
/** @internal */
export type ServeChatAttachmentSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
    "chat_sessions_token_header_Gram-Chat-Session"?: string | undefined;
};
/** @internal */
export declare const ServeChatAttachmentSecurity$outboundSchema: z.ZodMiniType<ServeChatAttachmentSecurity$Outbound, ServeChatAttachmentSecurity>;
export declare function serveChatAttachmentSecurityToJSON(serveChatAttachmentSecurity: ServeChatAttachmentSecurity): string;
/** @internal */
export type ServeChatAttachmentRequest$Outbound = {
    id: string;
    project_id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Chat-Session"?: string | undefined;
};
/** @internal */
export declare const ServeChatAttachmentRequest$outboundSchema: z.ZodMiniType<ServeChatAttachmentRequest$Outbound, ServeChatAttachmentRequest>;
export declare function serveChatAttachmentRequestToJSON(serveChatAttachmentRequest: ServeChatAttachmentRequest): string;
/** @internal */
export declare const ServeChatAttachmentResponse$inboundSchema: z.ZodMiniType<ServeChatAttachmentResponse, unknown>;
export declare function serveChatAttachmentResponseFromJSON(jsonString: string): SafeParseResult<ServeChatAttachmentResponse, SDKValidationError>;
//# sourceMappingURL=servechatattachment.d.ts.map