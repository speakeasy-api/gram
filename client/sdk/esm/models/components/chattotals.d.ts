import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Trace-entry counts across the entire returned generation, independent of pagination. Each message maps to exactly one entry: a message carrying tool calls counts as a tool call regardless of role, otherwise the role decides.
 */
export type ChatTotals = {
    /**
     * Number of assistant messages (without tool calls) in the generation.
     */
    assistantMessages: number;
    /**
     * Number of messages with an active (found, non-suppressed) risk finding in the generation.
     */
    riskOnly: number;
    /**
     * Number of messages carrying tool calls in the generation.
     */
    toolCalls: number;
    /**
     * Number of tool-result messages in the generation.
     */
    toolResults: number;
    /**
     * Total trace entries in the generation (sum of the four entry-type counts; the `of N entries` denominator).
     */
    total: number;
    /**
     * Number of user messages in the generation.
     */
    userMessages: number;
};
/** @internal */
export declare const ChatTotals$inboundSchema: z.ZodMiniType<ChatTotals, unknown>;
export declare function chatTotalsFromJSON(jsonString: string): SafeParseResult<ChatTotals, SDKValidationError>;
//# sourceMappingURL=chattotals.d.ts.map