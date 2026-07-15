import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * One UTC day of token usage split by risk involvement
 */
export type RiskTokensPoint = {
    /**
     * Bucket start time in Unix nanoseconds (string for JS precision)
     */
    bucketTimeUnixNano: string;
    /**
     * Tokens from sessions with at least one active risk finding created in the query window
     */
    riskyTokens: number;
    /**
     * All session tokens in the bucket
     */
    totalTokens: number;
};
/** @internal */
export declare const RiskTokensPoint$inboundSchema: z.ZodMiniType<RiskTokensPoint, unknown>;
export declare function riskTokensPointFromJSON(jsonString: string): SafeParseResult<RiskTokensPoint, SDKValidationError>;
//# sourceMappingURL=risktokenspoint.d.ts.map