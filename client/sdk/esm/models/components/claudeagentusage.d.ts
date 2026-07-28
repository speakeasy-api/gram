import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ClaudeToolUsage } from "./claudetoolusage.js";
import { ClaudeTurnUsage } from "./claudeturnusage.js";
export type ClaudeAgentUsage = {
  /**
   * Per-tool Claude usage keyed by tool_use_id.
   */
  tools: Array<ClaudeToolUsage>;
  /**
   * Per-prompt Claude usage turns ordered by start time.
   */
  turns: Array<ClaudeTurnUsage>;
};
/** @internal */
export declare const ClaudeAgentUsage$inboundSchema: z.ZodMiniType<
  ClaudeAgentUsage,
  unknown
>;
export declare function claudeAgentUsageFromJSON(
  jsonString: string,
): SafeParseResult<ClaudeAgentUsage, SDKValidationError>;
//# sourceMappingURL=claudeagentusage.d.ts.map
