import * as z from "zod/v4-mini";
export type SendMessageRequestBody = {
    /**
     * The assistant to send the message to.
     */
    assistantId: string;
    /**
     * The conversation to continue (from listChats or a prior sendMessage). Omit to start a new conversation; the server mints and returns a fresh chat id.
     */
    chatId?: string | undefined;
    /**
     * Stable key the client mints once per message so retries dedupe instead of enqueuing twice. A new key is generated server-side when omitted.
     */
    idempotencyKey?: string | undefined;
    /**
     * The user's message text.
     */
    message: string;
};
/** @internal */
export type SendMessageRequestBody$Outbound = {
    assistant_id: string;
    chat_id?: string | undefined;
    idempotency_key?: string | undefined;
    message: string;
};
/** @internal */
export declare const SendMessageRequestBody$outboundSchema: z.ZodMiniType<SendMessageRequestBody$Outbound, SendMessageRequestBody>;
export declare function sendMessageRequestBodyToJSON(sendMessageRequestBody: SendMessageRequestBody): string;
//# sourceMappingURL=sendmessagerequestbody.d.ts.map