import * as z from "zod/v4-mini";
import { SendMessageRequestBody, SendMessageRequestBody$Outbound } from "../components/sendmessagerequestbody.js";
export type SendAssistantMessageSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type SendAssistantMessageRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    sendMessageRequestBody: SendMessageRequestBody;
};
/** @internal */
export type SendAssistantMessageSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SendAssistantMessageSecurity$outboundSchema: z.ZodMiniType<SendAssistantMessageSecurity$Outbound, SendAssistantMessageSecurity>;
export declare function sendAssistantMessageSecurityToJSON(sendAssistantMessageSecurity: SendAssistantMessageSecurity): string;
/** @internal */
export type SendAssistantMessageRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SendMessageRequestBody: SendMessageRequestBody$Outbound;
};
/** @internal */
export declare const SendAssistantMessageRequest$outboundSchema: z.ZodMiniType<SendAssistantMessageRequest$Outbound, SendAssistantMessageRequest>;
export declare function sendAssistantMessageRequestToJSON(sendAssistantMessageRequest: SendAssistantMessageRequest): string;
//# sourceMappingURL=sendassistantmessage.d.ts.map