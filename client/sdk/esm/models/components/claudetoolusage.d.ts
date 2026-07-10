import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ClaudeToolUsage = {
    /**
     * Serialized tool input size in bytes.
     */
    inputSizeBytes: number;
    /**
     * Claude prompt.id for the turn that used this tool.
     */
    promptId: string;
    /**
     * Serialized tool result size in bytes.
     */
    resultSizeBytes: number;
    /**
     * Tool name reported by Claude Code.
     */
    toolName: string;
    /**
     * Claude tool_use_id that correlates the tool call and result.
     */
    toolUseId: string;
};
/** @internal */
export declare const ClaudeToolUsage$inboundSchema: z.ZodMiniType<ClaudeToolUsage, unknown>;
export declare function claudeToolUsageFromJSON(jsonString: string): SafeParseResult<ClaudeToolUsage, SDKValidationError>;
//# sourceMappingURL=claudetoolusage.d.ts.map