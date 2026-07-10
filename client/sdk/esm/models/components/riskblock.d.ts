import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Existing feedback sentiment recorded for this block, when any.
 */
export declare const RiskBlockFeedback: {
    readonly Up: "up";
    readonly Down: "down";
};
/**
 * Existing feedback sentiment recorded for this block, when any.
 */
export type RiskBlockFeedback = ClosedEnum<typeof RiskBlockFeedback>;
export type RiskBlock = {
    /**
     * When the block occurred.
     */
    createdAt: Date;
    /**
     * Existing feedback sentiment recorded for this block, when any.
     */
    feedback?: RiskBlockFeedback | undefined;
    /**
     * The block ID (the underlying risk result ID).
     */
    id: string;
    /**
     * Name of the risk policy that blocked the call.
     */
    policyName: string;
    /**
     * The project the block belongs to.
     */
    projectId: string;
    /**
     * Human-readable reason the tool call was blocked.
     */
    reason: string;
    /**
     * Name of the tool that was blocked, when known.
     */
    toolName?: string | undefined;
};
/** @internal */
export declare const RiskBlockFeedback$inboundSchema: z.ZodMiniEnum<typeof RiskBlockFeedback>;
/** @internal */
export declare const RiskBlock$inboundSchema: z.ZodMiniType<RiskBlock, unknown>;
export declare function riskBlockFromJSON(jsonString: string): SafeParseResult<RiskBlock, SDKValidationError>;
//# sourceMappingURL=riskblock.d.ts.map