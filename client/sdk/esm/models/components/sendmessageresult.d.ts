import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SendMessageResult = {
    /**
     * Whether the message was accepted and enqueued for processing.
     */
    accepted: boolean;
    /**
     * The chat to poll for the assistant's reply.
     */
    chatId: string;
    /**
     * The assistant thread the message was enqueued on, when the ingest produced one.
     */
    threadId?: string | undefined;
};
/** @internal */
export declare const SendMessageResult$inboundSchema: z.ZodMiniType<SendMessageResult, unknown>;
export declare function sendMessageResultFromJSON(jsonString: string): SafeParseResult<SendMessageResult, SDKValidationError>;
//# sourceMappingURL=sendmessageresult.d.ts.map