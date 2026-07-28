import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Usage breakdown by LLM client/agent
 */
export type LLMClientUsage = {
  /**
   * Number of messages (session mode) or tool calls (tool_call mode)
   */
  activityCount: number;
  /**
   * Client/agent name (e.g., 'cursor', 'claude-code', 'cowork')
   */
  clientName: string;
};
/** @internal */
export declare const LLMClientUsage$inboundSchema: z.ZodMiniType<
  LLMClientUsage,
  unknown
>;
export declare function llmClientUsageFromJSON(
  jsonString: string,
): SafeParseResult<LLMClientUsage, SDKValidationError>;
//# sourceMappingURL=llmclientusage.d.ts.map
