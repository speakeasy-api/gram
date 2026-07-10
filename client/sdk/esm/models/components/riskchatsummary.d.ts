import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskChatSummary = {
    /**
     * The chat session ID.
     */
    chatId: string;
    /**
     * Title of the chat session.
     */
    chatTitle?: string | undefined;
    /**
     * Number of findings in this chat.
     */
    findingsCount: number;
    /**
     * When the most recent finding was detected.
     */
    latestDetected: Date;
    /**
     * The user who owns the chat session.
     */
    userId?: string | undefined;
};
/** @internal */
export declare const RiskChatSummary$inboundSchema: z.ZodMiniType<RiskChatSummary, unknown>;
export declare function riskChatSummaryFromJSON(jsonString: string): SafeParseResult<RiskChatSummary, SDKValidationError>;
//# sourceMappingURL=riskchatsummary.d.ts.map