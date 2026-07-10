import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The LLM judge's verdict for one in-scope message in the replayed session.
 */
export type PromptGuardrailMessageVerdict = {
    /**
     * Completion tokens billed for this judge call.
     */
    completionTokens: number;
    /**
     * Judge confidence in [0,1]; 0 when not matched.
     */
    confidence: number;
    /**
     * OpenRouter cost for judging this message, in USD. Zero when cost was not returned.
     */
    costUsd: number;
    /**
     * Wall-clock latency for judging this message, in milliseconds.
     */
    latencyMs: number;
    /**
     * True when the guardrail flagged this message.
     */
    matched: boolean;
    /**
     * The chat message ID.
     */
    messageId: string;
    /**
     * The judged message type (user_message, assistant_message, tool_request, tool_response).
     */
    messageType: string;
    /**
     * Prompt tokens billed for this judge call.
     */
    promptTokens: number;
    /**
     * One-sentence judge rationale; empty when not matched.
     */
    rationale: string;
    /**
     * Message sequence within the chat generation, ascending.
     */
    seq: number;
    /**
     * Tool name for a single-call tool_request message; empty otherwise.
     */
    toolName?: string | undefined;
    /**
     * Total tokens billed for this judge call.
     */
    totalTokens: number;
};
/** @internal */
export declare const PromptGuardrailMessageVerdict$inboundSchema: z.ZodMiniType<PromptGuardrailMessageVerdict, unknown>;
export declare function promptGuardrailMessageVerdictFromJSON(jsonString: string): SafeParseResult<PromptGuardrailMessageVerdict, SDKValidationError>;
//# sourceMappingURL=promptguardrailmessageverdict.d.ts.map