import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Unified result for all Claude Code hook events with proper response structure
 */
export type ClaudeHookResult = {
    /**
     * Whether to continue (SessionStart only)
     */
    continue?: boolean | undefined;
    /**
     * Top-level block decision for UserPromptSubmit / PostToolUse / Stop / SubagentStop. Use 'block' to halt processing.
     */
    decision?: string | undefined;
    /**
     * Hook-specific output as JSON object
     */
    hookSpecificOutput?: any | undefined;
    /**
     * Reason accompanying decision; shown to the user (UserPromptSubmit) or Claude (PostToolUse/Stop).
     */
    reason?: string | undefined;
    /**
     * Reason if blocked (SessionStart only)
     */
    stopReason?: string | undefined;
    /**
     * Whether to suppress the hook's output
     */
    suppressOutput?: boolean | undefined;
    /**
     * Warning message shown to the user in the terminal
     */
    systemMessage?: string | undefined;
};
/** @internal */
export declare const ClaudeHookResult$inboundSchema: z.ZodMiniType<ClaudeHookResult, unknown>;
export declare function claudeHookResultFromJSON(jsonString: string): SafeParseResult<ClaudeHookResult, SDKValidationError>;
//# sourceMappingURL=claudehookresult.d.ts.map