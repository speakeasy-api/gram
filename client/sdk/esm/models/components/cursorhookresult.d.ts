import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result for Cursor hook events
 */
export type CursorHookResult = {
    /**
     * Additional context to inject into the conversation
     */
    additionalContext?: string | undefined;
    /**
     * Message sent back to the agent (beforeMCPExecution only)
     */
    agentMessage?: string | undefined;
    /**
     * Permission decision for preToolUse / beforeMCPExecution: allow, deny, or ask
     */
    permission?: string | undefined;
    /**
     * Message to display to the user
     */
    userMessage?: string | undefined;
};
/** @internal */
export declare const CursorHookResult$inboundSchema: z.ZodMiniType<CursorHookResult, unknown>;
export declare function cursorHookResultFromJSON(jsonString: string): SafeParseResult<CursorHookResult, SDKValidationError>;
//# sourceMappingURL=cursorhookresult.d.ts.map