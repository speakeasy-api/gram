import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Current state of product feature flags
 */
export type GramProductFeatures = {
    /**
     * Whether authz challenge logging to ClickHouse is enabled
     */
    authzChallengeLoggingEnabled: boolean;
    /**
     * Whether logging is enabled
     */
    logsEnabled: boolean;
    /**
     * Whether Claude Code session capture is enabled
     */
    sessionCaptureEnabled: boolean;
    /**
     * Whether tool I/O logging is enabled
     */
    toolIoLogsEnabled: boolean;
};
/** @internal */
export declare const GramProductFeatures$inboundSchema: z.ZodMiniType<GramProductFeatures, unknown>;
export declare function gramProductFeaturesFromJSON(jsonString: string): SafeParseResult<GramProductFeatures, SDKValidationError>;
//# sourceMappingURL=gramproductfeatures.d.ts.map