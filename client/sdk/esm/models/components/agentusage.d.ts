import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ClaudeAgentUsage } from "./claudeagentusage.js";
/**
 * The agent usage payload discriminator.
 */
export declare const Type: {
    readonly Claude: "claude";
};
/**
 * The agent usage payload discriminator.
 */
export type Type = ClosedEnum<typeof Type>;
export type AgentUsage = {
    claude?: ClaudeAgentUsage | undefined;
    /**
     * The agent usage payload discriminator.
     */
    type: Type;
};
/** @internal */
export declare const Type$inboundSchema: z.ZodMiniEnum<typeof Type>;
/** @internal */
export declare const AgentUsage$inboundSchema: z.ZodMiniType<AgentUsage, unknown>;
export declare function agentUsageFromJSON(jsonString: string): SafeParseResult<AgentUsage, SDKValidationError>;
//# sourceMappingURL=agentusage.d.ts.map