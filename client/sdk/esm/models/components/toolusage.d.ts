import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage statistics
 */
export type ToolUsage = {
    /**
     * Total call count
     */
    count: number;
    /**
     * Failed calls (4xx/5xx status)
     */
    failureCount: number;
    /**
     * Successful calls (2xx status)
     */
    successCount: number;
    /**
     * Tool URN
     */
    urn: string;
};
/** @internal */
export declare const ToolUsage$inboundSchema: z.ZodMiniType<ToolUsage, unknown>;
export declare function toolUsageFromJSON(jsonString: string): SafeParseResult<ToolUsage, SDKValidationError>;
//# sourceMappingURL=toolusage.d.ts.map